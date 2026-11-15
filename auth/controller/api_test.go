package controller_test

import (
	"crypto/sha256"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"avito-ai-buddy/auth/controller"
	handler "avito-ai-buddy/auth/controller/auth"
	"avito-ai-buddy/auth/entity"
	auth "avito-ai-buddy/auth/usecase/auth"
	"avito-ai-buddy/auth/usecase/auth/mocks"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

func TestLoginAndRefreshCookies(t *testing.T) {
	t.Parallel()

	const email, password = "test@example.com", "password123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)

	for _, prefix := range []string{"/v1/auth", "/api/v1/auth"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()

			repo := mocks.NewMockauthRepository(gomock.NewController(t))
			repo.EXPECT().GetUserByEmail(gomock.Any(), email).
				Return(entity.User{ID: 42, Email: email, PasswordHash: string(hash)}, nil)
			repo.EXPECT().CreateRefreshToken(gomock.Any(), int64(42), gomock.Any(), gomock.Any()).Return(nil)
			repo.EXPECT().RotateRefreshToken(gomock.Any(), int64(42), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil)
			logger := zap.NewNop()
			service := auth.NewAuthService(repo, logger, []byte("test-secret"))
			mux := http.NewServeMux()
			if prefix == "/v1/auth" {
				controller.New(service, logger).Register(mux)
			} else {
				handler.Register(mux, *handler.NewAuthHandler(service, logger))
			}

			login := httptest.NewRequest(http.MethodPost, "https://example.com"+prefix+"/login",
				strings.NewReader(`{"email":"test@example.com","password":"password123"}`))
			loginRecorder := httptest.NewRecorder()
			mux.ServeHTTP(loginRecorder, login)
			require.Equal(t, http.StatusOK, loginRecorder.Code, loginRecorder.Body.String())
			loginResponse := loginRecorder.Result()
			t.Cleanup(func() { require.NoError(t, loginResponse.Body.Close()) })
			cookies := loginResponse.Cookies()
			require.Len(t, cookies, 3)
			for _, cookie := range cookies {
				require.True(t, cookie.HttpOnly)
				require.True(t, cookie.Secure)
			}

			jar, err := cookiejar.New(nil)
			require.NoError(t, err)
			jar.SetCookies(login.URL, []*http.Cookie{{Name: "refresh_token", Value: "legacy", Path: path.Join(prefix, "refresh")}})
			jar.SetCookies(login.URL, cookies)
			refresh := httptest.NewRequest(http.MethodPost, "https://example.com"+prefix+"/refresh", nil)
			require.Len(t, jar.Cookies(refresh.URL), 2)
			for _, cookie := range jar.Cookies(refresh.URL) {
				refresh.AddCookie(cookie)
			}
			refreshRecorder := httptest.NewRecorder()
			mux.ServeHTTP(refreshRecorder, refresh)
			require.Equal(t, http.StatusOK, refreshRecorder.Code, refreshRecorder.Body.String())
			refreshResponse := refreshRecorder.Result()
			t.Cleanup(func() { require.NoError(t, refreshResponse.Body.Close()) })
			jar.SetCookies(refresh.URL, refreshResponse.Cookies())

			validate := httptest.NewRequest(http.MethodPost, "https://example.com"+prefix+"/validate", nil)
			for _, cookie := range jar.Cookies(validate.URL) {
				if cookie.Name == handler.AccessTokenCookie {
					validate.Header.Set("Authorization", "bearer "+cookie.Value)
				}
			}
			validateRecorder := httptest.NewRecorder()
			mux.ServeHTTP(validateRecorder, validate)
			require.Equal(t, http.StatusOK, validateRecorder.Code, validateRecorder.Body.String())
			require.JSONEq(t, `{"is_valid":true,"user_id":42}`, validateRecorder.Body.String())

			logout := httptest.NewRequest(http.MethodPost, "https://example.com"+prefix+"/logout", nil)
			for _, cookie := range jar.Cookies(logout.URL) {
				logout.AddCookie(cookie)
			}
			logout.Header.Set("Origin", "https://other.example")
			denied := httptest.NewRecorder()
			mux.ServeHTTP(denied, logout)
			require.Equal(t, http.StatusForbidden, denied.Code)
			require.Empty(t, denied.Header().Values("Set-Cookie"))

			cookie, err := logout.Cookie("refresh_token")
			require.NoError(t, err)
			hash := sha256.Sum256([]byte(cookie.Value))
			repo.EXPECT().DeleteRefreshToken(gomock.Any(), hash[:]).Return(nil)
			logout.Header.Set("Origin", "https://example.com")
			logoutRecorder := httptest.NewRecorder()
			mux.ServeHTTP(logoutRecorder, logout)
			require.Equal(t, http.StatusNoContent, logoutRecorder.Code)
			logoutResponse := logoutRecorder.Result()
			t.Cleanup(func() { require.NoError(t, logoutResponse.Body.Close()) })
			jar.SetCookies(logout.URL, logoutResponse.Cookies())
			require.Empty(t, jar.Cookies(refresh.URL))
		})
	}
}
