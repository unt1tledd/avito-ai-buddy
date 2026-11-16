package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	chaterrors "avito-ai-buddy/chat/internal/errors"
)

// Client validates access tokens against the auth service over HTTP.
type Client struct {
	client *http.Client
	url    string
}

// NewClient builds a client for the auth validate endpoint.
func NewClient(client *http.Client, baseURL, validatePath string) *Client {
	return &Client{client: client, url: strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(validatePath, "/")}
}

// ValidateToken checks the token with the auth service and returns the
// authenticated user ID. Invalid tokens are reported as ErrInvalidToken,
// transport and server failures as ErrAuthUnavailable.
func (c *Client) ValidateToken(ctx context.Context, token string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: build Auth validation request: %w", chaterrors.ErrAuthUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("%w: %v", chaterrors.ErrAuthUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return 0, chaterrors.ErrInvalidToken
	case http.StatusOK:
	default:
		return 0, fmt.Errorf("%w: HTTP %d", chaterrors.ErrAuthUnavailable, resp.StatusCode)
	}

	var result struct {
		IsValid bool  `json:"is_valid"`
		UserID  int64 `json:"user_id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil {
		return 0, fmt.Errorf("%w: decode Auth response: %w", chaterrors.ErrAuthUnavailable, err)
	}
	if !result.IsValid {
		return 0, chaterrors.ErrInvalidToken
	}
	if result.UserID <= 0 {
		return 0, fmt.Errorf("%w: invalid user ID in response", chaterrors.ErrAuthUnavailable)
	}
	return result.UserID, nil
}
