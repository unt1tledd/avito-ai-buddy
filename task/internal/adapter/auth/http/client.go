package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	taskerrors "avito-ai-buddy/task/internal/errors"
)

type Client struct {
	client           *http.Client
	url              string
	maxResponseBytes int64
}

func NewClient(client *http.Client, baseURL, validatePath string, maxResponseBytes int64) *Client {
	return &Client{client: client, url: strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(validatePath, "/"), maxResponseBytes: maxResponseBytes}
}

func (c *Client) ValidateToken(ctx context.Context, token string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: build validation request: %w", taskerrors.ErrAuthUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("%w: %w", taskerrors.ErrAuthUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return 0, taskerrors.ErrInvalidToken
	case http.StatusOK:
	default:
		return 0, fmt.Errorf("%w: HTTP %d", taskerrors.ErrAuthUnavailable, resp.StatusCode)
	}

	var result struct {
		IsValid bool  `json:"is_valid"`
		UserID  int64 `json:"user_id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, c.maxResponseBytes)).Decode(&result); err != nil {
		return 0, fmt.Errorf("%w: decode validation response: %w", taskerrors.ErrAuthUnavailable, err)
	}
	if !result.IsValid {
		return 0, taskerrors.ErrInvalidToken
	}
	if result.UserID <= 0 {
		return 0, fmt.Errorf("%w: Auth returned an invalid user ID", taskerrors.ErrAuthUnavailable)
	}
	return result.UserID, nil
}
