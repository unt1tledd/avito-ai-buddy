package events

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/controller/events/mocks"
	chaterrors "avito-ai-buddy/chat/internal/errors"

	"go.uber.org/mock/gomock"
)

func TestPublishRoutesUpdateToOwner(t *testing.T) {
	hub := NewHub()
	owner, unsubscribeOwner := hub.Subscribe("intern-a")
	defer unsubscribeOwner()
	other, unsubscribeOther := hub.Subscribe("intern-b")
	defer unsubscribeOther()
	handler := NewHandler(hub, nil, "task-secret")

	for _, tc := range []struct {
		token  string
		status int
	}{
		{"invalid-token", http.StatusUnauthorized},
		{"task-secret", http.StatusNoContent},
	} {
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/chat/task-events",
			strings.NewReader(`{"user_id":"intern-a","task_id":"task-42","status":"approved"}`))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		recorder := httptest.NewRecorder()
		handler.Publish(recorder, req)
		if recorder.Code != tc.status {
			t.Fatalf("status = %d, want %d", recorder.Code, tc.status)
		}
		if tc.status == http.StatusUnauthorized && len(owner) != 0 {
			t.Fatal("unauthorized request reached a subscriber")
		}
	}
	select {
	case update := <-owner:
		if update.TaskID != "task-42" || update.Status != "approved" {
			t.Fatalf("unexpected event: %+v", update)
		}
	default:
		t.Fatal("owner did not receive an event")
	}
	select {
	case <-other:
		t.Fatal("another user received the owner's event")
	default:
	}
}

func TestSubscribeRequiresSessionBeforeUpgrade(t *testing.T) {
	for _, token := range []string{"", "invalid"} {
		name := "missing token"
		if token != "" {
			name = "invalid token"
		}
		t.Run(name, func(t *testing.T) {
			sessions := mocks.NewMocksessionService(gomock.NewController(t))
			handler := NewHandler(NewHub(), sessions, "task-secret")
			req := httptest.NewRequest(http.MethodGet, "/v1/chat/events", nil)
			if token != "" {
				req.AddCookie(&http.Cookie{Name: "access_token", Value: token})
				sessions.EXPECT().GetSession(req.Context(), token).
					Return("", time.Time{}, chaterrors.ErrInvalidToken)
			}

			recorder := httptest.NewRecorder()
			handler.Subscribe(recorder, req)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestSubscribeReturnsServiceUnavailableWhenAuthFails(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/events", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: "session"})
	sessions := mocks.NewMocksessionService(gomock.NewController(t))
	sessions.EXPECT().GetSession(req.Context(), "session").
		Return("", time.Time{}, chaterrors.ErrAuthUnavailable)

	recorder := httptest.NewRecorder()
	NewHandler(NewHub(), sessions, "task-secret").Subscribe(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}
