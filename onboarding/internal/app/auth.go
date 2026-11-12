package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"

	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	planpb "avito-ai-buddy/pkg/generated/plan/v1"
	steppb "avito-ai-buddy/pkg/generated/step/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type tokenValidator interface {
	ValidateToken(context.Context, string) (int64, error)
}

func authorizeRequests(auth tokenValidator, serviceToken string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		headers := metadata.ValueFromIncomingContext(ctx, "authorization")
		if len(headers) != 1 {
			return nil, status.Error(codes.Unauthenticated, "Bearer token required")
		}
		scheme, token, ok := strings.Cut(headers[0], " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			return nil, status.Error(codes.Unauthenticated, "Bearer token required")
		}
		isServiceToken := serviceToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(serviceToken)) == 1

		switch info.FullMethod {
		case steppb.StepService_UpdateStepTemplate_FullMethodName:
			if !isServiceToken {
				return nil, status.Error(codes.Unauthenticated, "valid service token required")
			}
		case planpb.PlanService_AssignPlan_FullMethodName,
			planpb.PlanService_GetPlan_FullMethodName,
			planpb.PlanService_GetPlanContext_FullMethodName,
			steppb.StepService_GetCurrentStep_FullMethodName,
			steppb.StepService_CompleteStep_FullMethodName,
			steppb.StepService_ReopenStep_FullMethodName:
			if isServiceToken {
				return nil, status.Error(codes.Unauthenticated, "user access token required")
			}
			target, ok := req.(interface{ GetUserId() int64 })
			if !ok || target.GetUserId() <= 0 {
				return nil, status.Error(codes.InvalidArgument, "user ID must be positive")
			}
			userID, err := auth.ValidateToken(ctx, token)
			if err != nil {
				if errors.Is(err, onboardingerrors.ErrInvalidToken) {
					return nil, status.Error(codes.Unauthenticated, "invalid or expired access token")
				}
				if ctxStatus := status.FromContextError(err); ctxStatus.Code() != codes.Unknown {
					return nil, ctxStatus.Err()
				}
				return nil, status.Error(codes.Unavailable, "auth service unavailable")
			}
			if userID != target.GetUserId() {
				return nil, status.Error(codes.PermissionDenied, "onboarding plan belongs to another user")
			}
		default:
			return nil, status.Error(codes.PermissionDenied, "onboarding method is not authorized")
		}
		return handler(ctx, req)
	}
}
