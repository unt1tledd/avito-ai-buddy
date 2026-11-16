package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"

	userpb "avito-ai-buddy/pkg/generated/user/v1"
	"avito-ai-buddy/user/internal/entity"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type tokenValidator interface {
	ValidateToken(context.Context, string) (int64, error)
}

func authorizeRequests(auth tokenValidator, serviceToken string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info.FullMethod == healthpb.Health_Check_FullMethodName {
			return handler(ctx, req)
		}
		headers := metadata.ValueFromIncomingContext(ctx, "authorization")
		if len(headers) != 1 {
			return nil, status.Error(codes.Unauthenticated, "Bearer token required")
		}
		scheme, token, ok := strings.Cut(headers[0], " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			return nil, status.Error(codes.Unauthenticated, "Bearer token required")
		}
		isServiceToken := serviceToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(serviceToken)) == 1

		if info.FullMethod == userpb.UserService_GetMetadata_FullMethodName && isServiceToken {
			return handler(ctx, req)
		}
		if info.FullMethod != userpb.UserService_Create_FullMethodName && info.FullMethod != userpb.UserService_GetMetadata_FullMethodName {
			return nil, status.Error(codes.PermissionDenied, "user method is not authorized")
		}
		if isServiceToken {
			return nil, status.Error(codes.PermissionDenied, "user access token required for profile creation")
		}
		userID, err := auth.ValidateToken(ctx, token)
		if err != nil {
			if errors.Is(err, entity.ErrInvalidToken) {
				return nil, status.Error(codes.Unauthenticated, "invalid or expired access token")
			}
			return nil, status.Error(codes.Unavailable, "auth service unavailable")
		}
		target, ok := req.(interface{ GetUserId() int64 })
		if !ok || target.GetUserId() <= 0 {
			return nil, status.Error(codes.InvalidArgument, "user ID must be positive")
		}
		if userID != target.GetUserId() {
			return nil, status.Error(codes.PermissionDenied, "profile belongs to another user")
		}
		return handler(ctx, req)
	}
}
