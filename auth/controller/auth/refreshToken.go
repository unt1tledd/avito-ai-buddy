package auth

import (
	"errors"
	"net/http"

	"avito-ai-buddy/auth/entity"

	"go.uber.org/zap"
)

type RefreshResponse struct {
	AccessToken string `json:"access_token"`
}

func (a *authHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshTokenCookie)
	if err != nil {
		a.logger.Warn("refresh token cookie missing", zap.Error(err))
		a.respondError(w, http.StatusUnauthorized, "refresh token cookie missing")
		return
	}

	refreshToken := cookie.Value
	if refreshToken == "" {
		a.respondError(w, http.StatusUnauthorized, "refresh_token is empty")
		return
	}

	newAccess, newRefresh, err := a.authService.RefreshToken(r.Context(), refreshToken)
	if err != nil {
		if errors.Is(err, entity.ErrInvalidToken) {
			a.respondError(w, http.StatusUnauthorized, "invalid or expired refresh token")
			return
		}
		a.logger.Error("failed to refresh token", zap.Error(err))
		a.respondError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	setRefreshCookie(w, r, newRefresh)
	setAccessCookie(w, r, newAccess)

	a.respondJSON(w, http.StatusOK, RefreshResponse{
		AccessToken: newAccess,
	})
}
