package grpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	userpb "avito-ai-buddy/pkg/generated/user/v1"
)

type UserClient struct {
	serviceToken      string
	userServiceClient userpb.UserServiceClient
	timeout           time.Duration
}

func NewUserClient(userServiceClient userpb.UserServiceClient, timeout time.Duration, serviceTokens ...string) *UserClient {
	var token string
	if len(serviceTokens) > 0 {
		token = serviceTokens[0]
	}
	return &UserClient{serviceToken: token, userServiceClient: userServiceClient, timeout: timeout}
}

func (c *UserClient) GetTrack(ctx context.Context, userID int64) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	callCtx = metadata.AppendToOutgoingContext(callCtx, "authorization", "Bearer "+c.serviceToken)
	metadata, err := c.userServiceClient.GetMetadata(callCtx, &userpb.GetMetadataRequest{UserId: userID})
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		switch status.Code(err) {
		case codes.NotFound:
			return "", onboardingerrors.ErrUserMetadataNotFound
		case codes.DeadlineExceeded:
			return "", fmt.Errorf("get user metadata: %w", context.DeadlineExceeded)
		default:
			return "", fmt.Errorf("%w: %w", onboardingerrors.ErrUserUnavailable, err)
		}
	}
	track := strings.TrimSpace(metadata.GetTrack())
	if metadata.GetIntern().GetUserId() != userID || track == "" {
		return "", onboardingerrors.ErrInvalidUserMetadata
	}
	return track, nil
}
