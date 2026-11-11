package grpc

import (
	"context"
	"fmt"
	"strconv"

	"avito-ai-buddy/chat/internal/port"
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
func (c *Client) GetUserContext(ctx context.Context, userID string) (port.UserContext, error) {
	id, err := strconv.ParseInt(userID, 10, 64)
	if err != nil || id <= 0 {
		return port.UserContext{}, fmt.Errorf("invalid user id %q", userID)
	}

	metadata, err := c.client.GetMetadata(ctx, &userv1.GetMetadataRequest{UserId: id})
	if err != nil {
		return port.UserContext{}, fmt.Errorf("get user metadata: %w", err)
	}

	return port.UserContext{
		UserID: userID,
		TeamID: strconv.FormatInt(metadata.GetTeam().GetTeamId(), 10),
		Role:   "intern",
	}, nil
}
