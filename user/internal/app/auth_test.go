package app

import (
	"context"
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	userpb "avito-ai-buddy/pkg/generated/user/v1"
	"avito-ai-buddy/user/internal/entity"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type validatorStub struct{}

func (validatorStub) ValidateToken(_ context.Context, token string) (int64, error) {
	if token != "owner" {
		return 0, entity.ErrInvalidToken
	}
	return 42, nil
}
func TestUserAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, token string
		id          int64
		create      bool
		want        codes.Code
	}{
		{"anonymous read", "", 42, false, codes.Unauthenticated},
		{"anonymous create", "", 42, true, codes.Unauthenticated},
		{"invalid", "invalid", 42, false, codes.Unauthenticated},
		{"own read", "owner", 42, false, codes.OK},
		{"own create", "owner", 42, true, codes.OK},
		{"other read", "owner", 43, false, codes.PermissionDenied},
		{"other create", "owner", 43, true, codes.PermissionDenied},
		{"service read", "internal", 43, false, codes.OK},
		{"service create", "internal", 43, true, codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.token != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+tc.token))
			}
			var req any = &userpb.GetMetadataRequest{UserId: tc.id}
			method := userpb.UserService_GetMetadata_FullMethodName
			if tc.create {
				req = &userpb.CreateRequest{UserId: tc.id}
				method = userpb.UserService_Create_FullMethodName
			}
			called := false
			_, err := authorizeRequests(validatorStub{}, "internal")(ctx, req, &grpc.UnaryServerInfo{FullMethod: method}, func(context.Context, any) (any, error) { called = true; return nil, nil })
			if status.Code(err) != tc.want || called != (tc.want == codes.OK) {
				t.Fatalf("code=%v called=%v want=%v", status.Code(err), called, tc.want)
			}
		})
	}
}

type profileServer struct {
	userpb.UnimplementedUserServiceServer
}

func (profileServer) Create(context.Context, *userpb.CreateRequest) (*userpb.CreateResponse, error) {
	return &userpb.CreateResponse{InternshipId: 1}, nil
}
func (profileServer) GetMetadata(context.Context, *userpb.GetMetadataRequest) (*userpb.GetMetadataResponse, error) {
	return &userpb.GetMetadataResponse{}, nil
}

func TestGatewayAuthorization(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	defer func() { _ = listener.Close() }()
	server := grpc.NewServer(grpc.UnaryInterceptor(authorizeRequests(validatorStub{}, "internal")))
	userpb.RegisterUserServiceServer(server, profileServer{})
	defer server.Stop()
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///user", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	mux, err := newGateway(t.Context(), conn, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, body, token string
		want                      int
	}{
		{"POST", "/v1/user", `{"user_id":"42"}`, "", 401},
		{"POST", "/v1/user", `{"userId":"43"}`, "owner", 403},
		{"POST", "/v1/user", `{"user_id":"42"}`, "owner", 201},
		{"GET", "/v1/user/42/metadata", "", "", 401},
		{"GET", "/v1/user/43/metadata", "", "owner", 403},
		{"GET", "/v1/user/42/metadata", "", "owner", 200},
		{"GET", "/v1/user/43/metadata", "", "internal", 200},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %s: got %d want %d: %s", tc.method, tc.path, rec.Code, tc.want, rec.Body.String())
		}
	}
}
