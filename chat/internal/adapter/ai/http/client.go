package http

import (
	"context"
	"errors"
	"net/http"

	"avito-ai-buddy/chat/internal/port"
)

type Client struct {
	client  *http.Client
	baseURL string
}

func NewClient(client *http.Client, baseURL string) *Client {
	return &Client{client: client, baseURL: baseURL}
}

func (c *Client) GetAnswer(ctx context.Context, request port.AnswerRequest) (port.Answer, error) {
	return port.Answer{}, errors.ErrUnsupported
}
