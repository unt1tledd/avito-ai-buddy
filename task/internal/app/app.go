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

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"

	taskv1 "avito-ai-buddy/pkg/generated/task/v1"

	chatkafka "avito-ai-buddy/task/internal/adapter/chat/kafka"
	userhttp "avito-ai-buddy/task/internal/adapter/user/http"
	"avito-ai-buddy/task/internal/config"
	kafkaconsumer "avito-ai-buddy/task/internal/consumer/kafka"
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
	users := userhttp.NewClient(&http.Client{Timeout: 3 * time.Second}, cfg.UserBaseURL)
	service := taskusecase.NewService(repository, users)
	publisher := chatkafka.NewPublisher(cfg.Brokers(), cfg.TaskEventsTopic)
	defer func() { _ = publisher.Close() }()
	outboxWorker := outbox.New(logger, outboxrepo.NewPostgresRepository(db), publisher, cfg.Outbox)

	controller := taskctrl.NewController(service, logger, cfg.TrackerToken)
	grpcListener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen for tracker gRPC: %w", err)
	}
	defer func() { _ = grpcListener.Close() }()
	grpcServer := grpc.NewServer()
	taskv1.RegisterTaskServiceServer(grpcServer, controller)

	gatewayConn, err := connectGateway(grpcListener.Addr().(*net.TCPAddr))
	if err != nil {
		return fmt.Errorf("connect tracker gateway: %w", err)
	}
	defer func() { _ = gatewayConn.Close() }()
	server, err := newHTTPServer(ctx, gatewayConn)
	if err != nil {
		return err
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen for tracker HTTP: %w", err)
	}
	defer func() { _ = listener.Close() }()
	group, ctx := errgroup.WithContext(ctx)
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		shutdownErr := server.Shutdown(shutdownCtx)
		cancel()
		if shutdownErr != nil {
			_ = server.Close()
		}

		timer := time.AfterFunc(5*time.Second, grpcServer.Stop)
		defer timer.Stop()
		grpcServer.GracefulStop()
		if shutdownErr != nil {
			return fmt.Errorf("stop tracker HTTP: %w", shutdownErr)
		}
		return nil
	})
	group.Go(func() error {
		return kafkaconsumer.RunConsumer(ctx, logger, cfg.Brokers(), cfg.TaskCommandsTopic, cfg.KafkaConsumer,
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

func newHTTPServer(ctx context.Context, conn *grpc.ClientConn) (*http.Server, error) {
	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, trackerJSON{JSONPb: &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{UseProtoNames: true},
		}}),
		// Authorization is forwarded automatically. Ignore Grpc-Metadata-* headers.
		runtime.WithIncomingHeaderMatcher(func(string) (string, bool) { return "", false }),
	)
	if err := taskv1.RegisterTaskServiceHandler(ctx, mux, conn); err != nil {
		return nil, fmt.Errorf("register tracker HTTP routes: %w", err)
	}
	return &http.Server{
		Handler:           http.MaxBytesHandler(mux, 8<<10),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
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
