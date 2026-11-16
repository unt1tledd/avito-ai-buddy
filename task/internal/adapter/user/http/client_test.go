package http

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	taskerrors "avito-ai-buddy/task/internal/errors"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestGetTeamID(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantID    int64
		permanent bool
	}{
		{"metadata contains team", http.StatusOK, `{"team":{"team_id":"7"}}`, 7, false},
		{"missing internship is a permanent input error", http.StatusNotFound, "", 0, true},
		{"unavailable User can be retried", http.StatusServiceUnavailable, "", 0, false},
		{"malformed metadata", http.StatusOK, `{"team":`, 0, false},
		{"metadata without team", http.StatusOK, `{}`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.String() != "http://user.test/v1/user/42/metadata" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}, "http://user.test/", 64<<10)
			id, err := client.GetTeamID(t.Context(), 42)
			if id != tc.wantID || (err == nil) != (tc.wantID != 0) ||
				errors.Is(err, taskerrors.ErrInvalidInput) != tc.permanent {
				t.Fatalf("team=%d, err=%v, want team=%d, permanent=%t", id, err, tc.wantID, tc.permanent)
			}
		})
	}
}
