package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/controller/chat/mocks"
	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	chatpb "avito-ai-buddy/pkg/generated/chat/v1"

	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSendMessage(t *testing.T) {
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer session"))
	messages := mocks.NewMockmessageService(gomock.NewController(t))
	createdAt := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	messages.EXPECT().SendMessage(ctx, "session", "How do I get access?", int64(1001)).Return(entity.Message{
		ID: 12, Role: entity.MessageRoleAssistant, Text: "Request access", CreatedAt: createdAt,
		Sources:  []entity.Source{{DocumentID: "doc-1", Title: "Access guide", URL: "https://example.com/access"}},
		Proposal: &entity.Action{ID: 42, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read"},
	}, nil)

	got, err := NewServer(messages, zap.NewNop()).SendMessage(ctx, &chatpb.SendMessageRequest{
		Text: "How do I get access?", IdempotencyKey: 1001,
	})
	want := &chatpb.SendMessageResponse{AssistantMessage: &chatpb.Message{
		Id: "12", Role: chatpb.MessageRole_MESSAGE_ROLE_ASSISTANT, Text: "Request access",
		CreatedAt: timestamppb.New(createdAt),
		Sources:   []*chatpb.Source{{DocumentId: "doc-1", Title: "Access guide", Url: "https://example.com/access"}},
		Proposal:  &chatpb.AccessProposal{Id: "42", ResourceId: "repo", ResourceLabel: "Repository", AccessLevel: "read"},
	}}
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("SendMessage() = %v, %v, want %v, nil", got, err, want)
	}
}

func TestSendMessageErrors(t *testing.T) {
	for _, tc := range []struct {
		name           string
		text           string
		token          string
		idempotencyKey int64
		serviceErr     error
		want           codes.Code
	}{
		{name: "blank message", text: " \t", token: "session", idempotencyKey: 1001, want: codes.InvalidArgument},
		{name: "missing key", text: "question", token: "session", want: codes.InvalidArgument},
		{name: "negative key", text: "question", token: "session", idempotencyKey: -1, want: codes.InvalidArgument},
		{name: "missing token", text: "question", idempotencyKey: 1001, want: codes.Unauthenticated},
		{name: "invalid token", text: "question", token: "session", idempotencyKey: 1001, serviceErr: chaterrors.ErrInvalidToken, want: codes.Unauthenticated},
		{name: "auth unavailable", text: "question", token: "session", idempotencyKey: 1001, serviceErr: chaterrors.ErrAuthUnavailable, want: codes.Unavailable},
		{name: "cancelled", text: "question", token: "session", idempotencyKey: 1001, serviceErr: context.Canceled, want: codes.Canceled},
		{name: "key reused", text: "question", token: "session", idempotencyKey: 1001, serviceErr: chaterrors.ErrIdempotencyKeyReused, want: codes.AlreadyExists},
		{name: "reply in progress", text: "question", token: "session", idempotencyKey: 1001, serviceErr: chaterrors.ErrReplyInProgress, want: codes.Aborted},
		{name: "unexpected error", text: "question", token: "session", idempotencyKey: 1001, serviceErr: errors.New("AI unavailable"), want: codes.Internal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.token != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+tc.token))
			}
			messages := mocks.NewMockmessageService(gomock.NewController(t))
			if tc.serviceErr != nil {
				messages.EXPECT().SendMessage(ctx, tc.token, tc.text, tc.idempotencyKey).Return(entity.Message{}, tc.serviceErr)
			}

			got, err := NewServer(messages, zap.NewNop()).SendMessage(ctx, &chatpb.SendMessageRequest{
				Text: tc.text, IdempotencyKey: tc.idempotencyKey,
			})
			if got != nil || status.Code(err) != tc.want {
				t.Fatalf("SendMessage() = %v, %v, want nil, %s", got, err, tc.want)
			}
		})
	}
}

func TestGetHistory(t *testing.T) {
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("grpcgateway-cookie", "access_token=session"))
	messages := mocks.NewMockmessageService(gomock.NewController(t))
	messages.EXPECT().GetHistory(ctx, "session", int32(2), "18").Return(entity.HistoryPage{
		Messages: []entity.Message{
			{ID: 15, Role: entity.MessageRoleUser, Text: "Where is the guide?"},
			{ID: 16, Role: entity.MessageRoleAssistant, Text: "In the wiki"},
		},
		NextPageToken: "15",
	}, nil)

	got, err := NewServer(messages, zap.NewNop()).GetHistory(ctx, &chatpb.GetHistoryRequest{PageSize: 2, PageToken: "18"})
	want := &chatpb.GetHistoryResponse{
		Messages: []*chatpb.Message{
			{Id: "15", Role: chatpb.MessageRole_MESSAGE_ROLE_USER, Text: "Where is the guide?"},
			{Id: "16", Role: chatpb.MessageRole_MESSAGE_ROLE_ASSISTANT, Text: "In the wiki"},
		},
		NextPageToken: "15",
	}
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("GetHistory() = %v, %v, want %v, nil", got, err, want)
	}
}

func TestReopeningChatDistinguishesFailureFromEmptyHistory(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pageSize   int32
		signedIn   bool
		serviceErr error
		want       codes.Code
	}{
		{name: "invalid page size", pageSize: -1, signedIn: true, want: codes.InvalidArgument},
		{name: "signed out", want: codes.Unauthenticated},
		{name: "session revoked", signedIn: true, serviceErr: chaterrors.ErrInvalidToken, want: codes.Unauthenticated},
		{name: "auth unavailable", signedIn: true, serviceErr: chaterrors.ErrAuthUnavailable, want: codes.Unavailable},
		{name: "history request times out", signedIn: true, serviceErr: context.DeadlineExceeded, want: codes.DeadlineExceeded},
		{name: "history storage unavailable", signedIn: true, serviceErr: chaterrors.ErrListMessages, want: codes.Internal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.signedIn {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer session"))
			}
			messages := mocks.NewMockmessageService(gomock.NewController(t))
			if tc.serviceErr != nil {
				messages.EXPECT().GetHistory(ctx, "session", tc.pageSize, "").Return(entity.HistoryPage{}, tc.serviceErr)
			}

			got, err := NewServer(messages, zap.NewNop()).GetHistory(ctx, &chatpb.GetHistoryRequest{PageSize: tc.pageSize})
			if got != nil || status.Code(err) != tc.want {
				t.Fatalf("GetHistory() = %v, %v, want no history and %s", got, err, tc.want)
			}
		})
	}
}
