package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	taskerrors "avito-ai-buddy/task/internal/errors"
)

type Client struct {
	http    *http.Client
	baseURL string
}

func NewClient(client *http.Client, baseURL string) *Client {
	return &Client{http: client, baseURL: strings.TrimRight(baseURL, "/")}
}

func (c *Client) GetTeamID(ctx context.Context, userID int64) (int64, error) {
	endpoint := c.baseURL + "/v1/user/" + strconv.FormatInt(userID, 10) + "/metadata"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("%w: user %d has no internship", taskerrors.ErrInvalidInput, userID)
	}
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("User metadata returned HTTP %d", response.StatusCode)
	}
	var metadata struct {
		Team struct {
			ID int64 `json:"team_id,string"`
		} `json:"team"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&metadata); err != nil {
		return 0, fmt.Errorf("decode User metadata: %w", err)
	}
	if metadata.Team.ID <= 0 {
		return 0, errors.New("User metadata has no team")
	}
	return metadata.Team.ID, nil
}
