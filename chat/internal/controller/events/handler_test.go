package events

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/controller/events/mocks"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	"avito-ai-buddy/chat/internal/port"

	"go.uber.org/mock/gomock"
)

func TestPublishTaskStatusRoutesUpdateToOwner(t *testing.T) {
	hub := NewHub()
	owner, unsubscribeOwner := hub.Subscribe("intern-a")
	defer unsubscribeOwner()
	other, unsubscribeOther := hub.Subscribe("intern-b")
	defer unsubscribeOther()

	event := port.TaskEvent{UserID: "intern-a", TaskID: "task-42", ExternalRequestID: "access-42", Status: "approved"}
	if err := hub.PublishTaskStatus(context.Background(), event); err != nil {
		t.Fatalf("publish task status: %v", err)
	}
	select {
	case update := <-owner:
		want := StatusUpdate{TaskID: "task-42", ExternalRequestID: "access-42", Status: "approved"}
		if update.Type != EventTypeTaskStatusChanged || update.StatusUpdate != want {
			t.Fatalf("event = %+v, want type %q and payload %+v", update, EventTypeTaskStatusChanged, want)
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
			handler := NewHandler(NewHub(), sessions)
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
	NewHandler(NewHub(), sessions).Subscribe(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}
