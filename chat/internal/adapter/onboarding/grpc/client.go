package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc"

	"avito-ai-buddy/chat/internal/port"
)

type Client struct {
	conn grpc.ClientConnInterface
}

func NewClient(conn grpc.ClientConnInterface) *Client {
	return &Client{conn: conn}
}

func (c *Client) GetCurrentStep(ctx context.Context, userID string) (*port.OnboardingStep, error) {
	return nil, errors.ErrUnsupported
}
