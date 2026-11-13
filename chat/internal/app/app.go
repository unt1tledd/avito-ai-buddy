package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	aiadapter "avito-ai-buddy/chat/internal/adapter/ai/http"
	authadapter "avito-ai-buddy/chat/internal/adapter/auth/http"
	onboardingadapter "avito-ai-buddy/chat/internal/adapter/onboarding/grpc"
	taskkafka "avito-ai-buddy/chat/internal/adapter/task/kafka"
	useradapter "avito-ai-buddy/chat/internal/adapter/user/grpc"
	"avito-ai-buddy/chat/internal/config"
	kafkac "avito-ai-buddy/chat/internal/consumer/kafka"
	"avito-ai-buddy/chat/internal/controller"
	actionapi "avito-ai-buddy/chat/internal/controller/action"
	chatapi "avito-ai-buddy/chat/internal/controller/chat"
	"avito-ai-buddy/chat/internal/controller/events"
	"avito-ai-buddy/chat/internal/entity"
	inboxsvc "avito-ai-buddy/chat/internal/inbox"
	"avito-ai-buddy/chat/internal/outbox"
	"avito-ai-buddy/chat/internal/port"
	actionrepo "avito-ai-buddy/chat/internal/repository/action"
	conversationrepo "avito-ai-buddy/chat/internal/repository/conversation"
	inboxrepo "avito-ai-buddy/chat/internal/repository/inbox"
	messagerepo "avito-ai-buddy/chat/internal/repository/message"
	outboxrepo "avito-ai-buddy/chat/internal/repository/outbox"
	"avito-ai-buddy/chat/internal/usecase/action"
	"avito-ai-buddy/chat/internal/usecase/conversation"
	"avito-ai-buddy/chat/internal/usecase/message"
	"avito-ai-buddy/chat/internal/usecase/taskstatus"
	"avito-ai-buddy/chat/migrations"
	chatpb "avito-ai-buddy/pkg/generated/chat/v1"
	planv1 "avito-ai-buddy/pkg/generated/plan/v1"
	"avito-ai-buddy/pkg/health"
	"avito-ai-buddy/pkg/transactor"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Run boots the chat service: it wires the dependencies, serves gRPC and
// HTTP gateway, runs the outbox worker and the Kafka consumer, and blocks
// until the context is cancelled or a server fails.
func Run(ctx context.Context, logger *zap.Logger, cfg *config.Config) error {
	db, err := pgxpool.New(ctx, cfg.PG.URL)
	if err != nil {
		return fmt.Errorf("configure postgres pool: %w", err)
	}
	defer db.Close()
	if err := migrations.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate chat database: %w", err)
	}
	publisher, err := taskkafka.NewPublisher(cfg.Kafka.BrokerAddrs(), cfg.Kafka.TaskCommandsTopic)
	if err != nil {
		return err
	}
	defer publisher.Close()

	authClient := authadapter.NewClient(&http.Client{
		Timeout:       cfg.Auth.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, cfg.Auth.BaseURL, cfg.Auth.ValidatePath)
	opts := grpc.WithTransportCredentials(insecure.NewCredentials())
	hub := events.NewHub()
	grpcServer, messages, taskNotifier, closeDependencies, err := newChatServer(ctx, logger, cfg, opts, authClient, hub, db, publisher)
	if err != nil {
		return err
	}
	defer closeDependencies()
	defer grpcServer.Stop()
	healthServer := health.RegisterGRPC(grpcServer)

	grpcListener, err := net.Listen("tcp", ":"+cfg.GRPC.Port)
	if err != nil {
		return fmt.Errorf("listen for gRPC: %w", err)
	}
	defer func() { _ = grpcListener.Close() }()

	gatewayConn, err := connectGateway(grpcListener, opts)
	if err != nil {
		return err
	}
	defer func() { _ = gatewayConn.Close() }()

	socketCtx, cancelSockets := context.WithCancel(ctx)
	defer cancelSockets()
	consumerCtx, stopConsumer := context.WithCancel(ctx)
	defer stopConsumer()
	checkKafka := publisher.CheckTopics(cfg.Kafka.TaskCommandsTopic, cfg.Kafka.TaskEventsTopic, cfg.Kafka.DLQTopic)
	httpServer, err := newHTTPServer(consumerCtx, socketCtx, hub, gatewayConn, messages, db.Ping, checkKafka)
	if err != nil {
		return err
	}
	defer func() { _ = httpServer.Close() }()

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		defer stopConsumer()
		if err := kafkac.RunConsumer(consumerCtx, logger, &cfg.Kafka, taskNotifier); err != nil {
			logger.Error("task events consumer exited", zap.Error(err))
		}
	}()
	defer func() {
		stopConsumer()
		<-consumerDone
	}()

	httpListener, err := net.Listen("tcp", ":"+cfg.GRPC.GatewayPort)
	if err != nil {
		return fmt.Errorf("listen for HTTP: %w", err)
	}
	defer func() { _ = httpListener.Close() }()

	err = serveUntilStopped(ctx, logger, grpcServer, httpServer, grpcListener, httpListener)
	healthServer.Shutdown()
	cancelSockets()
	if err != nil {
		return err
	}
	return shutdownServers(grpcServer, httpServer)
}

type sessionService interface {
	GetSession(ctx context.Context, token string) (userID string, expiresAt time.Time, err error)
}

func newChatServer(ctx context.Context, logger *zap.Logger, cfg *config.Config, opts grpc.DialOption, authClient port.TokenValidator, hub *events.Hub, db *pgxpool.Pool, publisher *taskkafka.Publisher) (_ *grpc.Server, _ sessionService, _ port.TaskEventsNotifier, _ func(), err error) {
	var connections []*grpc.ClientConn
	closeDependencies := func() {
		for _, conn := range connections {
			_ = conn.Close()
		}
	}
	defer func() {
		if err != nil {
			closeDependencies()
		}
	}()

	connect := func(service, addr string) (*grpc.ClientConn, error) {
		conn, err := grpc.NewClient(addr, opts)
		if err != nil {
			return nil, fmt.Errorf("configure %s client: %w", service, err)
		}
		connections = append(connections, conn)
		return conn, nil
	}

	userConn, err := connect("User", cfg.Clients.UserGRPCAddr)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	planConn, err := connect("Onboarding", cfg.Clients.OnboardingGRPCAddr)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	tx := transactor.NewTransactor(db)
	inboxRepository := inboxrepo.NewPostgresRepository(db)
	outboxRepository := outboxrepo.NewPostgresRepository(db)
	worker := outbox.New(logger, outboxRepository, func(kind int32) (outbox.KindHandler, error) {
		if kind != entity.OutboxKindCreateAccessRequest {
			return nil, fmt.Errorf("unknown outbox message kind %d", kind)
		}
		return func(ctx context.Context, data []byte) error {
			var command port.CreateAccessRequest
			if err := json.Unmarshal(data, &command); err != nil {
				return fmt.Errorf("decode access request: %w", err)
			}
			return publisher.PublishCreateAccessRequest(ctx, command)
		}, nil
	}, cfg, tx, hub)
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Start(workerCtx, 1)
	}()
	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		ticker := time.NewTicker(cfg.Inbox.CleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				if err := inboxRepository.CleanupOlderThan(cleanupCtx, cfg.Inbox.Retention); err != nil {
					logger.Error("inbox retention cleanup failed", zap.Error(err))
				}
			}
		}
	}()
	closeServerDependencies := func() {
		stopWorker()
		<-workerDone
		stopCleanup()
		<-cleanupDone
		closeDependencies()
	}
	actions := actionrepo.NewPostgresRepository(db)
	conversations := conversation.NewService(conversationrepo.NewPostgresRepository(db))
	msgRepository := messagerepo.NewPostgresRepository(db)
	taskstatusService := taskstatus.New(logger, conversations, msgRepository, hub)
	taskNotifier := inboxsvc.New(inboxRepository, taskstatusService, tx)

	messages := message.NewService(
		conversations,
		msgRepository,
		actions,
		useradapter.NewClient(userConn),
		onboardingadapter.NewClient(planv1.NewPlanServiceClient(planConn)),
		aiadapter.NewClient(&http.Client{
			Timeout: cfg.Clients.AITimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}, aiadapter.Options{
			BaseURL: cfg.Clients.AIBaseURL, AnswerPath: cfg.Clients.AIAnswerPath,
			ServiceToken: cfg.Clients.AIServiceToken,
		}),
		authClient,
		tx,
	)
	chatController := chatapi.NewServer(messages, logger)
	actionController := actionapi.NewServer(action.NewService(actions, outboxRepository, authClient, tx), logger)

	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(messagerepo.WithHistoryPageSize(ctx, cfg.App.HistoryPageSize), req)
	}))
	chatpb.RegisterChatServiceServer(server, chatController)
	chatpb.RegisterActionServiceServer(server, actionController)
	return server, messages, taskNotifier, closeServerDependencies, nil
}

func connectGateway(listener net.Listener, opts grpc.DialOption) (*grpc.ClientConn, error) {
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("resolve gRPC listener port: %w", err)
	}
	conn, err := grpc.NewClient(net.JoinHostPort("localhost", port), opts)
	if err != nil {
		return nil, fmt.Errorf("configure HTTP gateway: %w", err)
	}
	return conn, nil
}

func newHTTPServer(ctx, socketCtx context.Context, hub *events.Hub, gatewayConn *grpc.ClientConn, messages sessionService, pingDB, checkKafka func(context.Context) error) (*http.Server, error) {
	mux := runtime.NewServeMux()
	if err := controller.RegisterHealth(ctx, mux, gatewayConn, pingDB, checkKafka); err != nil {
		return nil, fmt.Errorf("register health endpoint: %w", err)
	}
	if err := chatpb.RegisterChatServiceHandler(ctx, mux, gatewayConn); err != nil {
		return nil, fmt.Errorf("register Chat HTTP routes: %w", err)
	}

	if err := chatpb.RegisterActionServiceHandler(ctx, mux, gatewayConn); err != nil {
		return nil, fmt.Errorf("register Action HTTP routes: %w", err)
	}

	eventHandler := events.NewHandler(hub, messages)
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("GET /v1/chat/events", func(w http.ResponseWriter, r *http.Request) {
		requestCtx, cancel := context.WithCancel(r.Context())
		defer cancel()
		stop := context.AfterFunc(socketCtx, cancel)
		defer stop()

		eventHandler.Subscribe(w, r.WithContext(requestCtx))
	})
	httpMux.Handle("/", mux)

	return &http.Server{
		Handler:           httpMux,
		ReadHeaderTimeout: 5 * time.Second,
	}, nil
}

func serveUntilStopped(ctx context.Context, logger *zap.Logger, grpcServer *grpc.Server, httpServer *http.Server, grpcListener, httpListener net.Listener) error {
	errs := make(chan error, 2)
	go func() { errs <- grpcServer.Serve(grpcListener) }()
	go func() { errs <- httpServer.Serve(httpListener) }()
	logger.Info("chat servers listening", zap.String("grpc", grpcListener.Addr().String()), zap.String("http", httpListener.Addr().String()))

	select {
	case <-ctx.Done():
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, grpc.ErrServerStopped) {
			return err
		}
	}
	return nil
}

func shutdownServers(grpcServer *grpc.Server, httpServer *http.Server) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down HTTP gateway: %w", err)
	}

	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
		return nil
	case <-shutdownCtx.Done():
		return fmt.Errorf("shut down gRPC: %w", shutdownCtx.Err())
	}
}
