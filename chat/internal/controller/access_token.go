package controller

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AccessTokenCookie is the cookie holding the intern's access token.
const AccessTokenCookie = "access_token"

// GetAccessTokenFromContext extracts the access token from the gRPC request:
// first from the Authorization header, then from the cookies forwarded by
// the HTTP gateway. Without a token the request is unauthenticated.
func GetAccessTokenFromContext(ctx context.Context) (string, error) {
	token := ""
	for _, header := range metadata.ValueFromIncomingContext(ctx, "authorization") {
		scheme, value, ok := strings.Cut(header, " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			token = strings.TrimSpace(value)
			break
		}
	}
	if token == "" {
		cookies := metadata.ValueFromIncomingContext(ctx, "grpcgateway-cookie")
		if len(cookies) == 0 {
			cookies = metadata.ValueFromIncomingContext(ctx, "cookie")
		}
		request := &http.Request{Header: http.Header{"Cookie": cookies}}
		cookie, err := request.Cookie(AccessTokenCookie)
		if err != nil || cookie.Value == "" {
			return "", status.Error(codes.Unauthenticated, "access token required")
		}
		token = cookie.Value
	}

	return token, nil
}
