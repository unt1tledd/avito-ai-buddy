package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"avito-ai-buddy/auth/entity"

	"go.uber.org/zap"
)

type ValidateTokenRequest struct {
	Token string `json:"token"`
}

type ValidateTokenResponse struct {
	IsValid bool  `json:"is_valid"`
	UserID  int64 `json:"user_id"`
}

func (a *authHandler) ValidateToken(w http.ResponseWriter, r *http.Request) {
	var token string

	scheme, credentials, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if ok && strings.EqualFold(scheme, "Bearer") {
		token = strings.TrimSpace(credentials)
	}

	if token == "" && r.Body != nil && r.Body != http.NoBody {
		var req ValidateTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			a.respondError(w, http.StatusBadRequest, "invalid request payload")
			return
		}
		token = req.Token
	}

	if token == "" {
		a.respondError(w, http.StatusBadRequest, "token is required")
		return
	}

	isValid, userID, err := a.authService.ValidateToken(r.Context(), token)
	if err != nil {
		switch {
		case errors.Is(err, entity.ErrInvalidToken):
			a.respondError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		default:
			a.logger.Error(
				"failed to validate token",
				zap.Error(err),
			)
			a.respondError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	a.respondJSON(w, http.StatusOK, ValidateTokenResponse{
		IsValid: isValid,
		UserID:  userID,
	})
}
