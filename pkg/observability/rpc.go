package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const RequestIDHeader = "X-Request-ID"

type requestIDKey struct{}

var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func validOrNew(id string) string {
	if requestIDPattern.MatchString(id) {
		return id
	}
	var bytes [16]byte
	_, _ = rand.Read(bytes[:])
	return hex.EncodeToString(bytes[:])
}

// HTTPRequestID passes a correlation ID to the gateway and returns it to the caller.
func HTTPRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := validOrNew(r.Header.Get(RequestIDHeader))
		r.Header.Set(RequestIDHeader, id)
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func RPC(log func(ctx context.Context, method string, code codes.Code, duration time.Duration)) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		var id string
		if values := metadata.ValueFromIncomingContext(ctx, "x-request-id"); len(values) == 1 {
			id = values[0]
		}
		ctx = context.WithValue(ctx, requestIDKey{}, validOrNew(id))
		started := time.Now()
		response, err := handler(ctx, req)
		log(ctx, info.FullMethod, status.Code(err), time.Since(started))
		return response, err
	}
}
