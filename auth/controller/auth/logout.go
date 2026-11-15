package auth

import (
	"net/http"
	"path"

	"go.uber.org/zap"
)

var logoutOriginProtection http.CrossOriginProtection

func (a *authHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := logoutOriginProtection.Check(r); err != nil {
		a.respondError(w, http.StatusForbidden, "cross-origin logout is not allowed")
		return
	}

	if cookie, err := r.Cookie(refreshTokenCookie); err == nil {
		if err := a.authService.Logout(r.Context(), cookie.Value); err != nil {
			a.logger.Error("failed to logout user", zap.Error(err))
			a.respondError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	cookiePath := path.Dir(r.URL.Path)
	clearAuthCookie(w, r, AccessTokenCookie, "/")
	clearAuthCookie(w, r, refreshTokenCookie, cookiePath)
	clearAuthCookie(w, r, refreshTokenCookie, path.Join(cookiePath, "refresh"))
	w.WriteHeader(http.StatusNoContent)
}
