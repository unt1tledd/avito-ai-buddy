package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/signal"
	"syscall"
	"time"

	userv1 "avito-ai-buddy/pkg/generated/user/v1"
	"avito-ai-buddy/pkg/health"
	"avito-ai-buddy/user/internal/config"
	"avito-ai-buddy/user/internal/controller"
	userrepo "avito-ai-buddy/user/internal/repository/user"
	userusecase "avito-ai-buddy/user/internal/usecase/user"
	"avito-ai-buddy/user/migrations"

	"avito-ai-buddy/pkg/observability"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func Run(logger *zap.Logger, cfg *config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pgConfig, err := pgxpool.ParseConfig(cfg.ConstructPostgresURL())
	if err != nil {
		return fmt.Errorf("configure PostgreSQL: %w", err)
	}
	pgConfig.MaxConns = 8
	pgConfig.MinConns = 1
	pgConfig.HealthCheckPeriod = config.HealthCheckPeriod
	pgConfig.MaxConnLifetime = 0
	pgConfig.MaxConnIdleTime = config.MaxConnIdleTime

	db, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	defer db.Close()
	if err := migrations.SetupPostgres(ctx, db); err != nil {
		return fmt.Errorf("migrate PostgreSQL: %w", err)
	}
	if cfg.SeedDemo {
		if err := migrations.SeedDemo(ctx, db); err != nil {
			return fmt.Errorf("seed user demo data: %w", err)
		}
	}

	repository := userrepo.NewPostgresRepository(db)
	service := userusecase.NewUserService(repository)
	ctrl := controller.New(service, logger)

	group, ctx := errgroup.WithContext(ctx)
	grpcReady := make(chan string, 1)
	group.Go(func() error {
		return runRest(ctx, logger, cfg, grpcReady, db.Ping)
	})
	group.Go(func() error {
		return runGrpc(ctx, logger, cfg, ctrl, grpcReady)
	})
	return group.Wait()
}

func runRest(ctx context.Context, logger *zap.Logger, cfg *config.Config, grpcReady <-chan string, pingDB func(context.Context) error) error {
	var endpoint string
	select {
	case endpoint = <-grpcReady:
	case <-ctx.Done():
		return nil
	}

	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("create user gateway client: %w", err)
	}
	defer func() { _ = conn.Close() }()

	mux, err := newGateway(ctx, conn, pingDB)
	if err != nil {
		return fmt.Errorf("register user gateway: %w", err)
	}
	addr := net.JoinHostPort(cfg.GRPC.Host, cfg.GRPC.GatewayPort)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	shutdownDone := make(chan error, 1)
	stopShutdown := context.AfterFunc(ctx, func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			shutdownDone <- fmt.Errorf("shut down HTTP gateway: %w", err)
			return
		}
		shutdownDone <- nil
	})

	logger.Info("HTTP gateway starting", zap.String("addr", addr))
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	} else if err != nil {
		err = fmt.Errorf("serve HTTP gateway: %w", err)
	}
	if !stopShutdown() {
		return errors.Join(err, <-shutdownDone)
	}
	return err
}

func runGrpc(ctx context.Context, logger *zap.Logger, cfg *config.Config, ctrl *controller.API, grpcReady chan<- string) error {
	var listenerConfig net.ListenConfig
	addr := net.JoinHostPort(cfg.GRPC.Host, cfg.GRPC.Port)
	listener, err := listenerConfig.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen for gRPC: %w", err)
	}
	defer func() { _ = listener.Close() }()

	server := grpc.NewServer(grpc.UnaryInterceptor(observability.RPC(func(ctx context.Context, method string, code codes.Code, elapsed time.Duration) {
		log := logger.Info
		switch code {
		case codes.Internal, codes.Unavailable, codes.Unknown:
			log = logger.Error
		case codes.DeadlineExceeded:
			log = logger.Warn
		}
		log("RPC completed", zap.String("rpc", method), zap.String("code", code.String()),
			zap.Int64("duration_ms", elapsed.Milliseconds()), zap.String("request_id", observability.RequestID(ctx)))
	})))
	ctrl.Register(server)
	healthServer := health.RegisterGRPC(server)
	stopShutdown := context.AfterFunc(ctx, func() {
		healthServer.Shutdown()
		timer := time.AfterFunc(config.ShutdownTimeout, server.Stop)
		defer timer.Stop()
		server.GracefulStop()
	})
	defer stopShutdown()

	grpcReady <- gatewayTarget(listener.Addr().(*net.TCPAddr))
	logger.Info("gRPC server starting", zap.String("addr", listener.Addr().String()))
	if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("serve gRPC: %w", err)
	}
	return nil
}

func newGateway(ctx context.Context, conn *grpc.ClientConn, pingDB func(context.Context) error) (*runtime.ServeMux, error) {
	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{UseProtoNames: true, EmitDefaultValues: true},
		}),
		runtime.WithForwardResponseOption(func(_ context.Context, w http.ResponseWriter, response proto.Message) error {
			if _, ok := response.(*userv1.CreateResponse); ok {
				w.WriteHeader(http.StatusCreated)
			}
			return nil
		}),
	)
	if err := controller.RegisterHealth(ctx, mux, conn, pingDB); err != nil {
		return nil, fmt.Errorf("register health endpoint: %w", err)
	}
	if err := userv1.RegisterUserServiceHandler(ctx, mux, conn); err != nil {
		return nil, err
	}
	return mux, nil
}

func gatewayTarget(addr *net.TCPAddr) string {
	target := *addr
	if target.IP.IsUnspecified() {
		target.IP = net.IPv6loopback
		if addr.IP.To4() != nil {
			target.IP = net.IPv4(127, 0, 0, 1)
		}
	}
	return (&url.URL{Scheme: "passthrough", Path: "/" + target.String()}).String()
}
