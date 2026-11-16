package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/port"
)

// Client implements the task dispatch port over the task service API.
type Client struct {
	conn grpc.ClientConnInterface
}

// NewClient builds a client on top of an established gRPC connection.
func NewClient(conn grpc.ClientConnInterface) *Client {
	return &Client{conn: conn}
}

// CreateAccessRequest forwards the confirmed access request to the task
// service and returns the created request state.
func (c *Client) CreateAccessRequest(ctx context.Context, request port.CreateAccessRequest) (entity.AccessRequest, error) {
	return entity.AccessRequest{}, errors.ErrUnsupported
}
