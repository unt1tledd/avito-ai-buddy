package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	chaterrors "avito-ai-buddy/chat/internal/errors"
)

type authTransport func(*http.Request) (*http.Response, error)

func (f authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOnlyVerifiedInternIdentityCanOpenChat(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantID  int64
		wantErr error
	}{
		{name: "valid intern", status: http.StatusOK, body: `{"is_valid":true,"user_id":7}`, wantID: 7},
		{name: "revoked session", status: http.StatusOK, body: `{"is_valid":false,"user_id":7}`, wantErr: chaterrors.ErrInvalidToken},
		{name: "expired token", status: http.StatusUnauthorized, wantErr: chaterrors.ErrInvalidToken},
		{name: "access forbidden", status: http.StatusForbidden, wantErr: chaterrors.ErrInvalidToken},
		{name: "auth unavailable", status: http.StatusBadGateway, wantErr: chaterrors.ErrAuthUnavailable},
		{name: "broken auth response", status: http.StatusOK, body: "invalid JSON", wantErr: chaterrors.ErrAuthUnavailable},
		{name: "no verified identity", status: http.StatusOK, body: `{"is_valid":true}`, wantErr: chaterrors.ErrAuthUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: authTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.String() != "http://auth.test/v1/auth/validate" {
					t.Fatalf("unexpected Auth request: %s %s", req.Method, req.URL)
				}
				if req.Header.Get("Authorization") != "Bearer intern-session" || req.Header.Get("Accept") != "application/json" {
					t.Fatalf("unexpected Auth headers: %v", req.Header)
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			gotID, err := NewClient(client, "http://auth.test/", "/v1/auth/validate").ValidateToken(t.Context(), "intern-session")
			if gotID != tc.wantID || !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateToken() = %d, %v, want %d, %v", gotID, err, tc.wantID, tc.wantErr)
			}
		})
	}
}

func TestAuthFailureDoesNotGrantAccess(t *testing.T) {
	for _, tc := range []struct {
		name    string
		baseURL string
		cancel  bool
		wantErr error
	}{
		{name: "auth host unreachable", baseURL: "http://auth.test", wantErr: chaterrors.ErrAuthUnavailable},
		{name: "auth is misconfigured", baseURL: "://invalid", wantErr: chaterrors.ErrAuthUnavailable},
		{name: "intern cancelled request", baseURL: "http://auth.test", cancel: true, wantErr: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			client := &http.Client{Transport: authTransport(func(req *http.Request) (*http.Response, error) {
				if err := req.Context().Err(); err != nil {
					return nil, err
				}
				return nil, errors.New("connection refused")
			})}
			gotID, err := NewClient(client, tc.baseURL, "/validate").ValidateToken(ctx, "intern-session")
			if gotID != 0 || !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateToken() = %d, %v, want no identity and %v", gotID, err, tc.wantErr)
			}
		})
	}
}
