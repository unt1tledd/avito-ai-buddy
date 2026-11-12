package grpc

import (
	"context"
	"fmt"
	"strconv"

	"avito-ai-buddy/chat/internal/entity"
	userv1 "avito-ai-buddy/pkg/generated/user/v1"

	"google.golang.org/grpc"
)

// Client implements the user context port over the user service gRPC API.
type Client struct {
	client userv1.UserServiceClient
}

// NewClient builds a client on top of an established gRPC connection.
func NewClient(conn grpc.ClientConnInterface) *Client {
	return &Client{client: userv1.NewUserServiceClient(conn)}
}

// GetUserContext fetches the intern profile snippet needed by the AI service:
// the user and team IDs plus the intern role.
func (c *Client) GetUserContext(ctx context.Context, userID string) (entity.UserContext, error) {
	id, err := strconv.ParseInt(userID, 10, 64)
	if err != nil || id <= 0 {
		return entity.UserContext{}, fmt.Errorf("invalid user id %q", userID)
	}

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
