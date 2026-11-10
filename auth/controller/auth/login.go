package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"avito-ai-buddy/auth/entity"

	"go.uber.org/zap"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	AccessToken string `json:"access_token"`
}

func (a *authHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		a.respondError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	accessToken, refreshToken, err := a.authService.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, entity.ErrInvalidCredentials):
			a.respondError(w, http.StatusUnauthorized, "invalid email or password")
			return
		default:
			a.logger.Error("failed to login user", zap.Error(err))
			a.respondError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}
	setRefreshCookie(w, r, refreshToken)
	setAccessCookie(w, r, accessToken)
	a.respondJSON(w, http.StatusOK, LoginResponse{
		AccessToken: accessToken,
	})
}
