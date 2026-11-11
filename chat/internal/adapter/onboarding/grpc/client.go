package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc"

	"avito-ai-buddy/chat/internal/port"
)

// Client implements the onboarding step port over the onboarding service
// gRPC API.
type Client struct {
	conn grpc.ClientConnInterface
}

// NewClient builds a client on top of an established gRPC connection.
func NewClient(conn grpc.ClientConnInterface) *Client {
	return &Client{conn: conn}
}

// GetCurrentStep returns the onboarding step the intern is currently working
// on, or nil when the onboarding plan is finished.
func (c *Client) GetCurrentStep(ctx context.Context, userID string) (*port.OnboardingStep, error) {
	return nil, errors.ErrUnsupported
}
