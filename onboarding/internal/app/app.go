package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"avito-ai-buddy/pkg/observability"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"

	authadapter "avito-ai-buddy/onboarding/internal/adapter/auth/http"
	useradapter "avito-ai-buddy/onboarding/internal/adapter/user/grpc"
	"avito-ai-buddy/onboarding/internal/config"
	"avito-ai-buddy/onboarding/internal/controller"
	planapi "avito-ai-buddy/onboarding/internal/controller/plan"
	stepapi "avito-ai-buddy/onboarding/internal/controller/step"
	planrepo "avito-ai-buddy/onboarding/internal/repository/plan"
	plansqlc "avito-ai-buddy/onboarding/internal/repository/plan/sqlc"
	steprepo "avito-ai-buddy/onboarding/internal/repository/step"
	stepsqlc "avito-ai-buddy/onboarding/internal/repository/step/sqlc"
	"avito-ai-buddy/onboarding/internal/usecase/plan"
	"avito-ai-buddy/onboarding/internal/usecase/step"
	"avito-ai-buddy/onboarding/migrations"
	planpb "avito-ai-buddy/pkg/generated/plan/v1"
	steppb "avito-ai-buddy/pkg/generated/step/v1"
	userpb "avito-ai-buddy/pkg/generated/user/v1"
	"avito-ai-buddy/pkg/health"
	"avito-ai-buddy/pkg/transactor"
)

func Run(ctx context.Context, logger *zap.Logger, cfg *config.Config) error {
	db, err := pgxpool.New(ctx, cfg.PG.URL)
	if err != nil {
		return fmt.Errorf("configure onboarding PostgreSQL: %w", err)
	}
	defer db.Close()
	if err := migrations.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate onboarding database: %w", err)
	}
	if cfg.App.SeedDemo {
		if err := migrations.SeedDemo(ctx, db); err != nil {
			return fmt.Errorf("seed onboarding demo data: %w", err)
		}
	}

	grpcListener, err := net.Listen("tcp", ":"+cfg.GRPC.Port)
	if err != nil {
		return fmt.Errorf("listen for onboarding gRPC: %w", err)
	}
	defer func() { _ = grpcListener.Close() }()
	httpListener, err := net.Listen("tcp", ":"+cfg.GRPC.GatewayPort)
	if err != nil {
		return fmt.Errorf("listen for onboarding HTTP: %w", err)
	}
	defer func() { _ = httpListener.Close() }()

	userConn, err := grpc.NewClient(cfg.Clients.UserGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("configure User gRPC client: %w", err)
	}
	defer func() { _ = userConn.Close() }()
	userServiceClient := userpb.NewUserServiceClient(userConn)
	userClient := useradapter.NewUserClient(userServiceClient, cfg.Clients.UserTimeout)

	planRepository := planrepo.NewPlanRepository(plansqlc.New(db), transactor.NewTransactor(db))
	stepRepository := steprepo.NewStepRepository(stepsqlc.New(db))
	planService := plan.NewPlanService(planRepository, stepRepository, userClient)
	stepService := step.NewStepService(planRepository, stepRepository)
	planController := planapi.NewPlanController(planService, logger)
	stepController := stepapi.NewStepController(stepService, logger)

	authClient := authadapter.NewClient(&http.Client{
		Timeout: cfg.Auth.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, cfg.Auth.BaseURL, cfg.Auth.ValidatePath)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(observability.RPC(func(ctx context.Context, method string, code codes.Code, elapsed time.Duration) {
		log := logger.Info
		switch code {
		case codes.Internal, codes.Unavailable, codes.Unknown:
			log = logger.Error
		case codes.DeadlineExceeded:
			log = logger.Warn
		}
		log("RPC completed", zap.String("rpc", method), zap.String("code", code.String()),
			zap.Int64("duration_ms", elapsed.Milliseconds()), zap.String("request_id", observability.RequestID(ctx)))
	}), authorizeRequests(authClient, cfg.App.ServiceToken)))
	defer server.Stop()
	planpb.RegisterPlanServiceServer(server, planController)
	steppb.RegisterStepServiceServer(server, stepController)
	healthServer := health.RegisterGRPC(server)

	endpoint := net.JoinHostPort("localhost", fmt.Sprint(grpcListener.Addr().(*net.TCPAddr).Port))
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("configure onboarding gateway: %w", err)
	}
	defer func() { _ = conn.Close() }()
	gateway, err := newGateway(ctx, conn, db.Ping)
	if err != nil {
		return err
	}
	httpServer := &http.Server{Handler: gateway, ReadHeaderTimeout: cfg.App.HTTPReadHeaderTimeout}
	defer func() { _ = httpServer.Close() }()

	errs := make(chan error, 2)
	go func() { errs <- server.Serve(grpcListener) }()
	go func() { errs <- httpServer.Serve(httpListener) }()
	logger.Info("onboarding servers listening", zap.String("grpc", grpcListener.Addr().String()), zap.String("http", httpListener.Addr().String()))

	select {
	case <-ctx.Done():
		healthServer.Shutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	}
}

func newGateway(ctx context.Context, conn *grpc.ClientConn, pingDB func(context.Context) error) (*runtime.ServeMux, error) {
	mux := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{MarshalOptions: protojson.MarshalOptions{UseProtoNames: true}}))
	if err := controller.RegisterHealth(ctx, mux, conn, pingDB); err != nil {
		return nil, fmt.Errorf("register health endpoint: %w", err)
	}
	if err := planpb.RegisterPlanServiceHandler(ctx, mux, conn); err != nil {
		return nil, fmt.Errorf("register plan gateway: %w", err)
	}
	if err := steppb.RegisterStepServiceHandler(ctx, mux, conn); err != nil {
		return nil, fmt.Errorf("register step gateway: %w", err)
	}
	return mux, nil
}
