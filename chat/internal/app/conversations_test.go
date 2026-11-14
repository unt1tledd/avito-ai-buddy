package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type deniedSession struct{}

func (deniedSession) GetSession(context.Context, string) (string, time.Time, error) {
	return "", time.Time{}, errors.New("invalid token")
}
func TestConversationRoutesRequireSession(t *testing.T) {
	for _, method := range []string{"GET", "POST", "DELETE"} {
		mux := http.NewServeMux()
		registerConversationRoutes(mux, nil, deniedSession{})
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, httptest.NewRequest(method, "/v1/chat/conversations", nil))
		if res.Code != 401 {
			t.Fatalf("%s status=%d", method, res.Code)
		}
	}
}
