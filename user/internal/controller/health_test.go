package controller_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"avito-ai-buddy/user/internal/controller"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

func TestHealth(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stopGRPC   bool
		shutdown   bool
		databaseOK bool
		dbTimeout  bool
		wantStatus int
		wantBody   string
	}{
		{name: "ready", databaseOK: true, wantStatus: http.StatusOK, wantBody: `{"status":"ok"}`},
		{name: "postgres unavailable", wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"unavailable"}`},
		{name: "postgres timeout", dbTimeout: true, wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"unavailable"}`},
		{name: "grpc not serving", shutdown: true, wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"unavailable"}`},
		{name: "grpc unavailable", stopGRPC: true, wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"unavailable"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			t.Cleanup(func() { _ = listener.Close() })
			server := grpc.NewServer()
			healthServer := health.NewServer()
			healthpb.RegisterHealthServer(server, healthServer)
			if tc.shutdown {
				healthServer.Shutdown()
			}
			if tc.stopGRPC {
				_ = listener.Close()
			} else {
				t.Cleanup(server.Stop)
				go func() { _ = server.Serve(listener) }()
			}

			conn, err := grpc.NewClient("passthrough:///user",
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
					return listener.DialContext(ctx)
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			dbChecked := false
			mux := runtime.NewServeMux()
			err = controller.RegisterHealth(t.Context(), mux, conn, func(ctx context.Context) error {
				dbChecked = true
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("database readiness check has no deadline")
				}
				if tc.dbTimeout {
					<-ctx.Done()
					return ctx.Err()
				}
				if tc.databaseOK {
					return nil
				}
				return errors.New("database unavailable")
			})
			if err != nil {
				t.Fatal(err)
			}

			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

			if dbChecked != (!tc.shutdown && !tc.stopGRPC) {
				t.Errorf("unexpected database check state: %t", dbChecked)
			}
			if recorder.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, tc.wantStatus)
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if got := recorder.Body.String(); got != tc.wantBody {
				t.Errorf("body = %q, want %q", got, tc.wantBody)
			}
		})
	}
}
