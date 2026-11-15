package conversation

import (
	"context"
	"testing"
	"time"

	chatpb "avito-ai-buddy/pkg/generated/chat/v1"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type unexpectedSession struct{}

func (unexpectedSession) GetSession(context.Context, string) (string, time.Time, error) {
	panic("a request without credentials must not reach session validation")
}

func TestConversationRPCsRequireCredentials(t *testing.T) {
	server := NewServer(nil, unexpectedSession{}, zap.NewNop())
	ctx := t.Context()
	_, listErr := server.ListConversations(ctx, &chatpb.ListConversationsRequest{})
	_, createErr := server.CreateConversation(ctx, &chatpb.CreateConversationRequest{})
	_, deleteErr := server.DeleteConversation(ctx, &chatpb.DeleteConversationRequest{Id: 1})
	_, deleteAllErr := server.DeleteAllConversations(ctx, &chatpb.DeleteAllConversationsRequest{})
	for method, err := range map[string]error{
		"list": listErr, "create": createErr, "delete": deleteErr, "delete all": deleteAllErr,
	} {
		if status.Code(err) != codes.Unauthenticated {
			t.Errorf("%s: got %v, want Unauthenticated", method, err)
		}
	}
}
