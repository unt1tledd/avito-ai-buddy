package http

import (
	"context"
	"errors"
	"net/http"

	"avito-ai-buddy/chat/internal/port"
)

// Client calls the AI service over HTTP to produce assistant answers.
type Client struct {
	client  *http.Client
	baseURL string
}

// NewClient builds a client for the AI service base URL.
func NewClient(client *http.Client, baseURL string) *Client {
	return &Client{client: client, baseURL: baseURL}
}

// GetAnswer asks the AI service for an assistant reply to the question with
// the conversation history and onboarding context.
func (c *Client) GetAnswer(ctx context.Context, request port.AnswerRequest) (port.Answer, error) {
	return port.Answer{}, errors.ErrUnsupported
}
