package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"avito-ai-buddy/auth/controller/auth"
	"avito-ai-buddy/auth/entity"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type authServiceStub struct {
	auth.AuthService
	register func(email, password string) (int64, string, error)
	validate func(token string) (bool, int64, error)
	refresh  func(token string) (string, string, error)
}

func (s authServiceStub) Register(_ context.Context, email, password string) (int64, string, error) {
	return s.register(email, password)
}

func (s authServiceStub) ValidateToken(_ context.Context, token string) (bool, int64, error) {
	return s.validate(token)
}

func (s authServiceStub) RefreshToken(_ context.Context, token string) (string, string, error) {
	return s.refresh(token)
}

func TestRegister(t *testing.T) {
	t.Parallel()

	const validBody = `{"email":"test@example.com","password":"password123"}`
	tests := []struct {
		name       string
		body       string
		wantCalls  int
		serviceErr error
		status     int
		response   string
	}{
		{"created", validBody, 1, nil, http.StatusCreated, `{"user_id":42,"message":"registered"}`},
		{"malformed JSON", `{`, 0, nil, http.StatusBadRequest, `{"error":"invalid request body"}`},
		{"duplicate email", validBody, 1, entity.ErrUserAlreadyExists, http.StatusConflict, `{"error":"user already exists"}`},
		{"invalid password", validBody, 1, entity.ErrInvalidPassword, http.StatusBadRequest, `{"error":"password is too short or invalid"}`},
		{"invalid email", validBody, 1, entity.ErrInvalidEmail, http.StatusBadRequest, `{"error":"invalid email format"}`},
		{"service failure", validBody, 1, errors.New("database unavailable"), http.StatusInternalServerError, `{"error":"internal server error"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			service := authServiceStub{register: func(email, password string) (int64, string, error) {
				calls++
				require.Equal(t, "test@example.com", email)
				require.Equal(t, "password123", password)
				return 42, "registered", tt.serviceErr
			}}
			mux := http.NewServeMux()
			auth.Register(mux, *auth.NewAuthHandler(service, zap.NewNop()))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(tt.body))
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)

			require.Equal(t, tt.wantCalls, calls)
			require.Equal(t, tt.status, response.Code)
			require.Equal(t, "application/json", response.Header().Get("Content-Type"))
			require.JSONEq(t, tt.response, response.Body.String())
		})
	}
}

func TestValidateToken(t *testing.T) {
	t.Parallel()

	const validBody = `{"token":"body-token"}`
	tests := []struct {
		name          string
		authorization string
		body          string
		token         string
		serviceErr    error
		status        int
		response      string
	}{
		{"bearer takes precedence", "bearer   header-token  ", validBody, "header-token", nil, http.StatusOK, `{"is_valid":true,"user_id":42}`},
		{"JSON fallback", "Basic ignored", validBody, "body-token", nil, http.StatusOK, `{"is_valid":true,"user_id":42}`},
		{"malformed JSON", "", `{`, "", nil, http.StatusBadRequest, `{"error":"invalid request payload"}`},
		{"missing token", "", `{}`, "", nil, http.StatusBadRequest, `{"error":"token is required"}`},
		{"invalid token", "", validBody, "body-token", entity.ErrInvalidToken, http.StatusUnauthorized, `{"error":"invalid or expired token"}`},
		{"service failure", "", validBody, "body-token", errors.New("JWT secret unavailable"), http.StatusInternalServerError, `{"error":"internal server error"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			service := authServiceStub{validate: func(token string) (bool, int64, error) {
				calls++
				require.Equal(t, tt.token, token)
				return true, 42, tt.serviceErr
			}}
			mux := http.NewServeMux()
			auth.Register(mux, *auth.NewAuthHandler(service, zap.NewNop()))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/validate", strings.NewReader(tt.body))
			request.Header.Set("Authorization", tt.authorization)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)

			if tt.token == "" {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
			require.Equal(t, tt.status, response.Code)
			require.JSONEq(t, tt.response, response.Body.String())
		})
	}
}

func TestRefreshToken(t *testing.T) {
	t.Parallel()

	const oldToken = "old-refresh-token"
	tests := []struct {
		name       string
		cookie     *http.Cookie
		serviceErr error
		status     int
		response   string
	}{
		{"rotated tokens", &http.Cookie{Name: "refresh_token", Value: oldToken}, nil, http.StatusOK, `{"access_token":"new-access-token"}`},
		{"missing cookie", nil, nil, http.StatusUnauthorized, `{"error":"refresh token cookie missing"}`},
		{"empty cookie", &http.Cookie{Name: "refresh_token"}, nil, http.StatusUnauthorized, `{"error":"refresh_token is empty"}`},
		{"invalid token", &http.Cookie{Name: "refresh_token", Value: oldToken}, entity.ErrInvalidToken, http.StatusUnauthorized, `{"error":"invalid or expired refresh token"}`},
		{"service failure", &http.Cookie{Name: "refresh_token", Value: oldToken}, errors.New("JWT secret unavailable"), http.StatusInternalServerError, `{"error":"internal server error"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			service := authServiceStub{refresh: func(token string) (string, string, error) {
				calls++
				require.Equal(t, oldToken, token)
				return "new-access-token", "new-refresh-token", tt.serviceErr
			}}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
			wantCalls := 0
			if tt.cookie != nil {
				request.AddCookie(tt.cookie)
				if tt.cookie.Value != "" {
					wantCalls = 1
				}
			}
			response := httptest.NewRecorder()
			auth.NewAuthHandler(service, zap.NewNop()).RefreshToken(response, request)

			require.Equal(t, wantCalls, calls)
			require.Equal(t, tt.status, response.Code)
			require.JSONEq(t, tt.response, response.Body.String())
			cookies := (&http.Response{Header: response.Header()}).Cookies()
			if tt.status == http.StatusOK {
				require.Len(t, cookies, 3)
				values := make(map[string]string, len(cookies))
				for _, cookie := range cookies {
					values[cookie.Name+" "+cookie.Path] = cookie.Value
					if cookie.Path == "/api/v1/auth/refresh" {
						require.Equal(t, -1, cookie.MaxAge)
					}
				}
				require.Equal(t, map[string]string{
					"refresh_token /api/v1/auth/refresh": "",
					"refresh_token /api/v1/auth":         "new-refresh-token",
					"access_token /":                     "new-access-token",
				}, values)
			} else {
				require.Empty(t, cookies)
			}
		})
	}
}
