package events

//go:generate mockgen -source=handler.go -destination=mocks/handler_mocks.go -package=mocks

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"avito-ai-buddy/chat/internal/controller"
	chaterrors "avito-ai-buddy/chat/internal/errors"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type sessionService interface {
	GetSession(ctx context.Context, token string) (userID string, expiresAt time.Time, err error)
}

type Handler struct {
	hub       *Hub
	sessions  sessionService
	taskToken string
}

func NewHandler(hub *Hub, sessions sessionService, taskToken string) *Handler {
	return &Handler{hub: hub, sessions: sessions, taskToken: taskToken}
}

func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(controller.AccessTokenCookie)
	if err != nil || cookie.Value == "" {
		http.Error(w, "access token required", http.StatusUnauthorized)
		return
	}
	userID, expiresAt, err := h.sessions.GetSession(r.Context(), cookie.Value)
	if errors.Is(err, chaterrors.ErrInvalidToken) {
		http.Error(w, chaterrors.ErrInvalidToken.Error(), http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "Auth service unavailable", http.StatusServiceUnavailable)
		return
	}
	updates, unsubscribe := h.hub.Subscribe(userID)
	defer unsubscribe()
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	session, cancel := context.WithDeadline(r.Context(), expiresAt)
	defer cancel()
	ctx := conn.CloseRead(session)
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case update, open := <-updates:
			if !open {
				return
			}
			event := struct {
				Type string `json:"type"`
				StatusUpdate
			}{Type: "task.status_changed", StatusUpdate: update}
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := wsjson.Write(writeCtx, conn, event)
			cancel()
			if err != nil {
				return
			}
		case <-heartbeat.C:
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	if h.taskToken == "" {
		http.Error(w, "task authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(h.taskToken)) != 1 {
		http.Error(w, "invalid task credentials", http.StatusUnauthorized)
		return
	}

	var request struct {
		UserID string `json:"user_id"`
		StatusUpdate
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := decoder.Decode(&request); err != nil ||
		decoder.Decode(new(json.RawMessage)) != io.EOF ||
		strings.TrimSpace(request.UserID) == "" ||
		strings.TrimSpace(request.TaskID) == "" ||
		strings.TrimSpace(request.Status) == "" {
		http.Error(w, "invalid status update", http.StatusBadRequest)
		return
	}
	h.hub.Publish(request.UserID, request.StatusUpdate)
	w.WriteHeader(http.StatusNoContent)
}
