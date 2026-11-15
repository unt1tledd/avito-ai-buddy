package grpc

import (
	"context"
	"fmt"
	"strconv"

	"avito-ai-buddy/chat/internal/entity"
	userv1 "avito-ai-buddy/pkg/generated/user/v1"
	"avito-ai-buddy/pkg/observability"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Client implements the user context port over the user service gRPC API.
type Client struct {
	serviceToken string
	client       userv1.UserServiceClient
}

// NewClient builds a client on top of an established gRPC connection.
func NewClient(conn grpc.ClientConnInterface, serviceTokens ...string) *Client {
	var token string
	if len(serviceTokens) > 0 {
		token = serviceTokens[0]
	}
	return &Client{serviceToken: token, client: userv1.NewUserServiceClient(conn)}
}

// GetUserContext fetches the intern profile snippet needed by the AI service:
// the user and team IDs plus the intern role.
func (c *Client) GetUserContext(ctx context.Context, userID string) (entity.UserContext, error) {
	id, err := strconv.ParseInt(userID, 10, 64)
	if err != nil || id <= 0 {
		return entity.UserContext{}, fmt.Errorf("invalid user id %q", userID)
	}

	if id := observability.RequestID(ctx); id != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", id)
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.serviceToken)
	metadata, err := c.client.GetMetadata(ctx, &userv1.GetMetadataRequest{UserId: id})
	if err != nil {
		return entity.UserContext{}, fmt.Errorf("get user metadata: %w", err)
	}

	teamID := metadata.GetTeam().GetTeamId()
	if teamID <= 0 {
		return entity.UserContext{}, fmt.Errorf("user metadata has no valid team")
	}
	return entity.UserContext{
		UserID:        userID,
		TeamID:        strconv.FormatInt(teamID, 10),
		Role:          "intern",
		ActualEndDate: metadata.GetActualEndDate(),
	}, nil
}
