package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/port"
)

type Client struct {
	conn grpc.ClientConnInterface
}

func NewClient(conn grpc.ClientConnInterface) *Client {
	return &Client{conn: conn}
}

func (c *Client) CreateAccessRequest(ctx context.Context, request port.CreateAccessRequest) (entity.AccessRequest, error) {
	return entity.AccessRequest{}, errors.ErrUnsupported
}
