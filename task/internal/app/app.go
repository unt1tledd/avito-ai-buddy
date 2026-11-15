package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"avito-ai-buddy/pkg/observability"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"

	taskv1 "avito-ai-buddy/pkg/generated/task/v1"
	"avito-ai-buddy/pkg/health"
	"avito-ai-buddy/pkg/transactor"

	authhttp "avito-ai-buddy/task/internal/adapter/auth/http"
	chatkafka "avito-ai-buddy/task/internal/adapter/chat/kafka"
	userhttp "avito-ai-buddy/task/internal/adapter/user/http"
	"avito-ai-buddy/task/internal/config"
	kafkaconsumer "avito-ai-buddy/task/internal/consumer/kafka"
	"avito-ai-buddy/task/internal/controller"
	taskctrl "avito-ai-buddy/task/internal/controller/task"
	"avito-ai-buddy/task/internal/outbox"
	outboxrepo "avito-ai-buddy/task/internal/repository/outbox"
	taskrepo "avito-ai-buddy/task/internal/repository/task"
	taskusecase "avito-ai-buddy/task/internal/usecase/task"
	"avito-ai-buddy/task/migrations"
)

func Run(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("configure PostgreSQL: %w", err)
	}
	defer db.Close()
	if err := migrations.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate PostgreSQL: %w", err)
	}

	repository := taskrepo.NewPostgresRepository(db)
	users := userhttp.NewClient(&http.Client{Timeout: cfg.UserTimeout}, cfg.UserBaseURL, cfg.UserMaxResponseBytes)
	service := taskusecase.NewService(repository, users)
	publisher := chatkafka.NewPublisher(cfg.Brokers(), cfg.TaskEventsTopic, cfg.KafkaWriter)
	defer func() { _ = publisher.Close() }()
	outboxWorker := outbox.New(logger, outboxrepo.PostgresRepository{}, transactor.NewTransactor(db), publisher, cfg.Outbox)

	auth := authhttp.NewClient(&http.Client{
		Timeout:       cfg.AuthTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, cfg.AuthBaseURL, "/v1/auth/validate", cfg.AuthMaxResponseBytes)
	taskController := taskctrl.NewController(service, logger, cfg.TrackerToken, auth, cfg.DefaultPageSize)
	grpcListener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen for tracker gRPC: %w", err)
	}
	defer func() { _ = grpcListener.Close() }()
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(observability.RPC(func(ctx context.Context, method string, code codes.Code, elapsed time.Duration) {
		level := slog.LevelInfo
		switch code {
		case codes.Internal, codes.Unavailable, codes.Unknown:
			level = slog.LevelError
		case codes.DeadlineExceeded:
			level = slog.LevelWarn
		}
		logger.Log(ctx, level, "RPC completed", "rpc", method, "code", code.String(),
			"duration_ms", elapsed.Milliseconds(), "request_id", observability.RequestID(ctx))
	})))
	taskv1.RegisterTaskServiceServer(grpcServer, taskController)
	healthServer := health.RegisterGRPC(grpcServer)

	gatewayConn, err := connectGateway(grpcListener.Addr().(*net.TCPAddr))
	if err != nil {
		return fmt.Errorf("connect tracker gateway: %w", err)
	}
	defer func() { _ = gatewayConn.Close() }()
	group, ctx := errgroup.WithContext(ctx)
	checkKafka := publisher.CheckTopics(cfg.TaskCommandsTopic, cfg.TaskEventsTopic, cfg.KafkaConsumer.DLQTopic)
	server, err := newHTTPServer(ctx, gatewayConn, cfg.HTTPServer, db.Ping, checkKafka)
	if err != nil {
		return err
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen for tracker HTTP: %w", err)
	}
	defer func() { _ = listener.Close() }()
	group.Go(func() error {
		logger.Info("task gRPC starting", "addr", grpcListener.Addr())
		if err := grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return fmt.Errorf("serve tracker gRPC: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		logger.Info("task HTTP starting", "addr", listener.Addr())
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve tracker HTTP: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		<-ctx.Done()
		healthServer.Shutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		shutdownErr := server.Shutdown(shutdownCtx)
		cancel()
		if shutdownErr != nil {
			_ = server.Close()
		}

		timer := time.AfterFunc(cfg.ShutdownTimeout, grpcServer.Stop)
		defer timer.Stop()
		grpcServer.GracefulStop()
		if shutdownErr != nil {
			return fmt.Errorf("stop tracker HTTP: %w", shutdownErr)
		}
		return nil
	})
	group.Go(func() error {
		return kafkaconsumer.RunConsumer(ctx, logger, cfg.Brokers(), cfg.TaskCommandsTopic, cfg.KafkaConsumer, cfg.KafkaWriter,
			kafkaconsumer.NewCreateHandler(service))
	})
	group.Go(func() error {
		outboxWorker.Start(ctx)
		return nil
	})
	return group.Wait()
}

func connectGateway(addr *net.TCPAddr) (*grpc.ClientConn, error) {
	target := *addr
	if target.IP.IsUnspecified() {
		target.IP = net.IPv6loopback
		if addr.IP.To4() != nil {
			target.IP = net.IPv4(127, 0, 0, 1)
		}
	}
	endpoint := (&url.URL{Scheme: "passthrough", Path: "/" + target.String()}).String()
	return grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy())
}

func newHTTPServer(ctx context.Context, conn *grpc.ClientConn, cfg config.HTTPServerConfig, pingDB, checkKafka func(context.Context) error) (*http.Server, error) {
	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, trackerJSON{JSONPb: &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{UseProtoNames: true},
		}}),
		// Authorization is forwarded automatically. Ignore Grpc-Metadata-* headers.
		runtime.WithIncomingHeaderMatcher(func(string) (string, bool) { return "", false }),
	)
	if err := controller.RegisterHealth(ctx, mux, conn, pingDB, checkKafka); err != nil {
		return nil, fmt.Errorf("register health endpoint: %w", err)
	}
	if err := taskv1.RegisterTaskServiceHandler(ctx, mux, conn); err != nil {
		return nil, fmt.Errorf("register tracker HTTP routes: %w", err)
	}
	return &http.Server{
		Handler:           http.MaxBytesHandler(mux, cfg.MaxBodyBytes),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}, nil
}

type trackerJSON struct {
	*runtime.JSONPb
}

func (m trackerJSON) Marshal(value any) ([]byte, error) {
	// An empty repeated field decodes as nil after a gRPC round trip.
	if tasks, ok := value.([]*taskv1.AccessRequest); ok && tasks == nil {
		return []byte("[]"), nil
	}
	return m.JSONPb.Marshal(value)
}
