package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"avito-ai-buddy/user/internal/entity"
)

type Client struct {
	client *http.Client
	url    string
}

func NewClient(client *http.Client, baseURL, validatePath string) *Client {
	return &Client{client: client, url: strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(validatePath, "/")}
}

func (c *Client) ValidateToken(ctx context.Context, token string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: build validation request: %w", entity.ErrAuthUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("%w: %w", entity.ErrAuthUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return 0, entity.ErrInvalidToken
	case http.StatusOK:
	default:
		return 0, fmt.Errorf("%w: HTTP %d", entity.ErrAuthUnavailable, resp.StatusCode)
	}

	var result struct {
		IsValid bool  `json:"is_valid"`
		UserID  int64 `json:"user_id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil {
		return 0, fmt.Errorf("%w: decode validation response: %w", entity.ErrAuthUnavailable, err)
	}
	if !result.IsValid {
		return 0, entity.ErrInvalidToken
	}
	if result.UserID <= 0 {
		return 0, fmt.Errorf("%w: Auth returned an invalid user ID", entity.ErrAuthUnavailable)
	}
	return result.UserID, nil
}
