package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const correlationID = "0123456789abcdef0123456789abcdef"

func TestHTTPRequestID(t *testing.T) {
	for _, incoming := range []string{correlationID, "malformed", ""} {
		t.Run(incoming, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(RequestIDHeader, incoming)
			res := httptest.NewRecorder()
			var forwarded string
			handler := HTTPRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				forwarded = RequestID(r.Context())
				if got := r.Header.Get(RequestIDHeader); got != forwarded {
					t.Errorf("forwarded header = %q, context ID = %q", got, forwarded)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			handler.ServeHTTP(res, req)

			if res.Code != http.StatusNoContent || res.Header().Get(RequestIDHeader) != forwarded {
				t.Fatalf("response = %d %v, want 204 with request ID %q", res.Code, res.Header(), forwarded)
			}
			if !requestIDPattern.MatchString(forwarded) || (incoming == correlationID && forwarded != incoming) {
				t.Errorf("forwarded %q for input %q", forwarded, incoming)
			}
		})
	}
}

func TestRPCLogsHandlerStatusWithRequestID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		code   codes.Code
	}{
		{"propagates ID", []string{correlationID}, codes.OK},
		{"replaces duplicate IDs", []string{correlationID, correlationID}, codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(t.Context(), metadata.MD{"x-request-id": tc.values})
			const method = "/example.Service/Get"
			var handledID string
			logged := false
			intercept := RPC(func(ctx context.Context, gotMethod string, code codes.Code, duration time.Duration) {
				logged = true
				if RequestID(ctx) != handledID || gotMethod != method || code != tc.code || duration < 0 {
					t.Errorf("logged request ID %q, method %q, code %v, duration %v", RequestID(ctx), gotMethod, code, duration)
				}
			})
			got, err := intercept(ctx, "request", &grpc.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, req any) (any, error) {
				handledID = RequestID(ctx)
				if req != "request" {
					t.Errorf("handler request = %v", req)
				}
				if tc.code != codes.OK {
					return nil, status.Error(tc.code, "backend unavailable")
				}
				return "response", nil
			})
			if !logged || !requestIDPattern.MatchString(handledID) || (handledID == correlationID) != (len(tc.values) == 1) {
				t.Errorf("handled ID = %q, logged = %v", handledID, logged)
			}
			if status.Code(err) != tc.code || (tc.code == codes.OK && got != "response") {
				t.Errorf("interceptor returned %v, %v", got, err)
			}
		})
	}
}
