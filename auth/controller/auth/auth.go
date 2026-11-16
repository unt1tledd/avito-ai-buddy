package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"path"

	"go.uber.org/zap"
)

type AuthService interface {
	Register(ctx context.Context, email, password string) (userID int64, message string, err error)
	Login(ctx context.Context, email, password string) (accessToken, refreshToken string, err error)
	Logout(ctx context.Context, refreshToken string) error
	RefreshToken(ctx context.Context, refreshToken string) (newAccessToken, newRefreshToken string, err error)
	ValidateToken(ctx context.Context, token string) (isValid bool, userID int64, err error)
}

type authHandler struct {
	authService AuthService
	logger      *zap.Logger
}

func NewAuthHandler(
	authService AuthService,
	logger *zap.Logger,
) *authHandler {
	return &authHandler{
		authService: authService,
		logger:      logger,
	}
}

func Register(mux *http.ServeMux, h authHandler) {
	mux.HandleFunc("POST /api/v1/auth/register", h.Register)
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)
	mux.HandleFunc("POST /api/v1/auth/logout", h.Logout)
	mux.HandleFunc("POST /api/v1/auth/validate", h.ValidateToken)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.RefreshToken)
}

type errorResponse struct {
	Error string `json:"error"`
}

const (
	AccessTokenCookie  = "access_token"
	refreshTokenCookie = "refresh_token"
)

func setAccessCookie(w http.ResponseWriter, r *http.Request, accessToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     AccessTokenCookie,
		Value:    accessToken,
		Path:     "/",
		MaxAge:   15 * 60,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func setRefreshCookie(w http.ResponseWriter, r *http.Request, refreshToken string) {
	cookiePath := path.Dir(r.URL.Path)
	clearAuthCookie(w, r, refreshTokenCookie, path.Join(cookiePath, "refresh"))
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenCookie,
		Value:    refreshToken,
		Path:     cookiePath,
		MaxAge:   7 * 24 * 60 * 60,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearAuthCookie(w http.ResponseWriter, r *http.Request, name, cookiePath string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Path:     cookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *authHandler) respondJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	if payload != nil {
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			a.logger.Error("failed to encode response json",
				zap.Int("status_code", code),
				zap.Error(err),
			)
		}
	}
}

func (a *authHandler) respondError(w http.ResponseWriter, code int, message string) {
	a.respondJSON(w, code, errorResponse{Error: message})
}
