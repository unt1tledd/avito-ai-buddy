package app

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/config"
	actionapi "avito-ai-buddy/chat/internal/controller/action"
	actionmocks "avito-ai-buddy/chat/internal/controller/action/mocks"
	chatapi "avito-ai-buddy/chat/internal/controller/chat"
	chatmocks "avito-ai-buddy/chat/internal/controller/chat/mocks"
	"avito-ai-buddy/chat/internal/entity"
	chatpb "avito-ai-buddy/pkg/generated/chat/v1"

	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestInternCanConfirmAndSeeThePendingAccessRequestInChat(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	const token = "intern-session"
	ctrl := gomock.NewController(t)
	actions := actionmocks.NewMockactionService(ctrl)
	messages := chatmocks.NewMockmessageService(ctrl)

	pending := entity.AccessRequest{Status: "pending_dispatch"}
	gomock.InOrder(
		messages.EXPECT().SendMessage(gomock.Any(), token, "How do I get access?", int64(math.MaxInt64)).Return(entity.Message{
			ID: 12, Role: entity.MessageRoleAssistant, Text: "You can request read access to the repository.",
			Proposal: &entity.Action{ID: 42, ResourceID: "repo", AccessLevel: "read"},
		}, nil),
		actions.EXPECT().ConfirmProposal(gomock.Any(), token, "42").Return(pending, nil),
		messages.EXPECT().GetHistory(gomock.Any(), token, int32(5), "").Return(entity.HistoryPage{
			Messages: []entity.Message{{
				ID: 12, Role: entity.MessageRoleAssistant, Text: "You can request read access to the repository.",
				Proposal: &entity.Action{ID: 42, ResourceID: "repo", AccessLevel: "read", AccessRequest: &pending},
			}},
		}, nil),
	)
	logger := zap.NewNop()
	grpcServer := grpc.NewServer()
	chatpb.RegisterActionServiceServer(grpcServer, actionapi.NewServer(actions, logger))
	chatpb.RegisterChatServiceServer(grpcServer, chatapi.NewServer(messages, logger))

	rpcListener := bufconn.Listen(1 << 16)
	httpListener := bufconn.Listen(1 << 16)
	conn, err := grpc.NewClient("passthrough:///chat", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return rpcListener.DialContext(ctx)
		}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	httpServer, err := newHTTPServer(ctx, ctx, &config.Config{}, conn, nil)
	if err != nil {
		t.Fatal(err)
	}
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- serveUntilStopped(ctx, logger, grpcServer, httpServer, rpcListener, httpListener)
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-serverResult; err != nil {
			t.Errorf("serve chat: %v", err)
		}
		if err := shutdownServers(grpcServer, httpServer); err != nil {
			t.Errorf("shut down chat: %v", err)
		}
	})

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return httpListener.DialContext(ctx) },
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}

	send, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://chat.test/v1/chat/messages",
		strings.NewReader(`{"text":"How do I get access?","idempotencyKey":"9223372036854775807"}`))
	if err != nil {
		t.Fatal(err)
	}
	send.Header.Set("Content-Type", "application/json")
	send.AddCookie(&http.Cookie{Name: "access_token", Value: token})
	var sent struct {
		AssistantMessage struct {
			Proposal struct{ ID string }
		}
	}
	readChatResponse(t, client, send, &sent)
	if sent.AssistantMessage.Proposal.ID != "42" {
		t.Fatalf("proposal ID = %q, want 42", sent.AssistantMessage.Proposal.ID)
	}

	confirm, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://chat.test/v1/chat/proposals/42/confirm", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	confirm.AddCookie(&http.Cookie{Name: "access_token", Value: token})
	var confirmed struct {
		AccessRequest struct {
			Status string
		}
	}
	readChatResponse(t, client, confirm, &confirmed)
	if confirmed.AccessRequest.Status != "pending_dispatch" {
		t.Fatalf("confirmation status = %q, want pending_dispatch", confirmed.AccessRequest.Status)
	}

	history, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://chat.test/v1/chat/messages?page_size=5", nil)
	if err != nil {
		t.Fatal(err)
	}
	history.AddCookie(&http.Cookie{Name: "access_token", Value: token})
	var page struct {
		Messages []struct {
			ID       string
			Proposal struct {
				ID            string
				AccessRequest struct {
					Status string
				}
			}
		}
	}
	readChatResponse(t, client, history, &page)
	if len(page.Messages) != 1 || page.Messages[0].ID != "12" || page.Messages[0].Proposal.ID != "42" ||
		page.Messages[0].Proposal.AccessRequest.Status != "pending_dispatch" {
		t.Fatalf("reopened chat = %+v, want pending request for proposal 42", page)
	}
}

func readChatResponse(t *testing.T, client *http.Client, req *http.Request, target any) {
	t.Helper()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s = %d, want %d", req.Method, req.URL.Path, resp.StatusCode, http.StatusOK)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("decode %s response: %v", req.URL.Path, err)
	}
}
