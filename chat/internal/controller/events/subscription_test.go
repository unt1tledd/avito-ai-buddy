package events

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/controller"
	"avito-ai-buddy/chat/internal/controller/events/mocks"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"go.uber.org/mock/gomock"
)

func TestInternReceivesApprovalOnAnOpenChat(t *testing.T) {
	hub := NewHub()
	otherIntern, unsubscribe := hub.Subscribe("8")
	defer unsubscribe()
	sessions := mocks.NewMocksessionService(gomock.NewController(t))
	sessions.EXPECT().GetSession(gomock.Any(), "intern-session").
		Return("7", time.Now().Add(time.Minute), nil)
	handler := NewHandler(hub, sessions, "task-secret")

	client := newPipeHTTPClient(t, http.HandlerFunc(handler.Subscribe))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	header := http.Header{"Cookie": {controller.AccessTokenCookie + "=intern-session"}}
	conn, handshake, err := websocket.Dial(ctx, "ws://chat.test/v1/chat/events", &websocket.DialOptions{
		HTTPClient: client, HTTPHeader: header,
	})
	if handshake != nil && handshake.Body != nil {
		defer func() { _ = handshake.Body.Close() }()
	}
	if err != nil {
		t.Fatalf("open chat events: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/chat/task-events",
		strings.NewReader(`{"user_id":"7","task_id":"task-42","external_request_id":"access-42","status":"approved"}`))
	req.Header.Set("Authorization", "Bearer task-secret")
	resp := httptest.NewRecorder()
	handler.Publish(resp, req)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("publish approval = %d, want %d", resp.Code, http.StatusNoContent)
	}

	var got struct {
		Type string `json:"type"`
		StatusUpdate
	}
	if err := wsjson.Read(ctx, conn, &got); err != nil {
		t.Fatalf("read approval event: %v", err)
	}
	want := StatusUpdate{TaskID: "task-42", ExternalRequestID: "access-42", Status: "approved"}
	if got.Type != "task.status_changed" || got.StatusUpdate != want {
		t.Fatalf("approval event = %+v, want task.status_changed %+v", got, want)
	}
	select {
	case update := <-otherIntern:
		t.Fatalf("another intern received the approval: %+v", update)
	default:
	}
}

func TestInvalidTaskCallbacksCannotChangeChatStatus(t *testing.T) {
	for _, tc := range []struct {
		name      string
		taskToken string
		body      string
		want      int
	}{
		{name: "task integration not configured", want: http.StatusServiceUnavailable},
		{name: "no recipient", taskToken: "task-secret", body: `{"task_id":"task-42","status":"approved"}`, want: http.StatusBadRequest},
		{name: "no status", taskToken: "task-secret", body: `{"user_id":"7","task_id":"task-42"}`, want: http.StatusBadRequest},
		{name: "extra JSON", taskToken: "task-secret", body: `{"user_id":"7","task_id":"task-42","status":"approved"}{}`, want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hub := NewHub()
			updates, unsubscribe := hub.Subscribe("7")
			defer unsubscribe()
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/chat/task-events", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer task-secret")
			resp := httptest.NewRecorder()
			NewHandler(hub, nil, tc.taskToken).Publish(resp, req)
			if resp.Code != tc.want {
				t.Fatalf("task callback = %d, want %d", resp.Code, tc.want)
			}
			select {
			case update := <-updates:
				t.Fatalf("rejected callback reached the chat: %+v", update)
			default:
			}
		})
	}
}

func TestSlowChatTabDoesNotBlockOtherTabs(t *testing.T) {
	hub := NewHub()
	slow, disconnectSlow := hub.Subscribe("7")
	defer disconnectSlow()
	active, disconnectActive := hub.Subscribe("7")
	defer disconnectActive()

	for i := 0; i <= cap(slow); i++ {
		want := StatusUpdate{TaskID: "task-42", Status: "approved"}
		hub.Publish("7", want)
		select {
		case got := <-active:
			if got != want {
				t.Fatalf("status update = %+v, want %+v", got, want)
			}
		default:
			t.Fatal("active tab missed the status update")
		}
	}
	for i := 0; i <= cap(slow); i++ {
		select {
		case _, open := <-slow:
			if !open {
				return
			}
		default:
			t.Fatal("slow tab remained subscribed after its buffer filled")
		}
	}
	t.Fatal("slow tab channel remained open")
}

type pipeListener struct {
	connections chan net.Conn
	done        chan struct{}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	close(l.done)
	return nil
}

func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{} }

// Use the standard HTTP server and WebSocket handshake without binding a port.
func newPipeHTTPClient(t *testing.T, handler http.Handler) *http.Client {
	t.Helper()
	listener := &pipeListener{connections: make(chan net.Conn), done: make(chan struct{})}
	server := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		<-done
	})
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			client, server := net.Pipe()
			select {
			case listener.connections <- server:
				return client, nil
			case <-ctx.Done():
				_ = server.Close()
				_ = client.Close()
				return nil, ctx.Err()
			}
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport}
}
