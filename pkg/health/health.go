package health

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	grpcHealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func RegisterGRPC(server *grpc.Server) *grpcHealth.Server {
	check := grpcHealth.NewServer()
	healthpb.RegisterHealthServer(server, check)
	return check
}

func RegisterGateway(appCtx context.Context, mux *runtime.ServeMux, conn grpc.ClientConnInterface, checks ...func(context.Context) error) error {
	client := healthpb.NewHealthClient(conn)
	return mux.HandlePath(http.MethodGet, "/health", func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")

		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		stop := context.AfterFunc(appCtx, cancel)
		defer stop()

		ready := func() bool {
			if appCtx.Err() != nil {
				return false
			}
			response, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
			if err != nil || response.GetStatus() != healthpb.HealthCheckResponse_SERVING {
				return false
			}
			for _, probe := range checks {
				if ctx.Err() != nil || probe(ctx) != nil {
					return false
				}
			}
			return ctx.Err() == nil && appCtx.Err() == nil
		}
		if !ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"status":"unavailable"}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
}
