package health_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"avito-ai-buddy/pkg/health"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type servingConnection struct {
	grpc.ClientConnInterface
}

func (servingConnection) Invoke(_ context.Context, _ string, _, response any, _ ...grpc.CallOption) error {
	response.(*healthpb.HealthCheckResponse).Status = healthpb.HealthCheckResponse_SERVING
	return nil
}

func TestGatewayChecksAllDependencies(t *testing.T) {
	appCtx, stop := context.WithCancel(t.Context())
	defer stop()

	mux := runtime.NewServeMux()
	var kafkaErr error
	kafkaChecks := 0
	if err := health.RegisterGateway(appCtx, mux, servingConnection{},
		func(context.Context) error { return nil },
		func(context.Context) error {
			kafkaChecks++
			return kafkaErr
		},
	); err != nil {
		t.Fatal(err)
	}

	check := func(want int) {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
		if response.Code != want {
			t.Fatalf("health status = %d, want %d", response.Code, want)
		}
	}

	kafkaErr = errors.New("Kafka unavailable")
	check(http.StatusServiceUnavailable)
	kafkaErr = nil
	check(http.StatusOK)
	stop()
	check(http.StatusServiceUnavailable)
	if kafkaChecks != 2 {
		t.Errorf("Kafka checks = %d, want 2", kafkaChecks)
	}
}
