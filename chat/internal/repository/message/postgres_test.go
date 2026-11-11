package message

import (
	"encoding/json"

	"errors"
	"reflect"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	"avito-ai-buddy/chat/internal/repository/message/mocks"
	sqlc "avito-ai-buddy/chat/internal/repository/message/sqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/mock/gomock"
)

func TestCreate(t *testing.T) {
	createdAt := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		role        entity.MessageRole
		sources     []entity.Source
		wantRole    sqlc.ChatMessageRole
		wantSources string
	}{
		{
			name: "assistant with sources", role: entity.MessageRoleAssistant,
			sources:  []entity.Source{{DocumentID: "doc-1", Title: "Guide", URL: "https://example.com/guide"}},
			wantRole: sqlc.ChatMessageRoleAssistant, wantSources: `[{"DocumentID":"doc-1","Title":"Guide","URL":"https://example.com/guide"}]`,
		},
		{name: "unspecified role", wantRole: sqlc.ChatMessageRoleUnspecified, wantSources: "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			queries := mocks.NewMockQuerier(gomock.NewController(t))
			queries.EXPECT().Create(ctx, sqlc.CreateParams{
				ConversationID: 21, Role: tc.wantRole, Body: "guide", Sources: []byte(tc.wantSources),
				CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
			}).Return(int64(42), nil)

			got, err := (&PostgresRepository{queries: queries}).Create(ctx, entity.Message{
				ConversationID: 21, Role: tc.role, Text: "guide", Sources: tc.sources, CreatedAt: createdAt,
			})
			if err != nil || got != 42 {
				t.Fatalf("Create() = %d, %v, want 42, nil", got, err)
			}
		})
	}
}

func TestListByConversation(t *testing.T) {
	ctx := WithHistoryPageSize(t.Context(), 2)
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	createdAt := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	timestamp := pgtype.Timestamptz{Time: createdAt, Valid: true}
	queries.EXPECT().ListByConversation(ctx, sqlc.ListByConversationParams{
		ConversationID: 21, BeforeID: 50, PageLimit: 3,
	}).Return([]sqlc.ListByConversationRow{
		{
			ID: 49, ConversationID: 21, Role: sqlc.ChatMessageRoleAssistant, Body: "guide", CreatedAt: timestamp,
			Sources:    []byte(`[{"DocumentID":"doc-1","Title":"Guide","URL":"https://example.com/guide"}]`),
			ProposalID: 42, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read",
			TaskID: "task-1", ExternalRequestID: "request-1", Status: "approved",
		},
		{ID: 48, ConversationID: 21, Role: sqlc.ChatMessageRoleUser, Body: "question", CreatedAt: timestamp, Sources: []byte("null")},
		{ID: 47, ConversationID: 21, Role: sqlc.ChatMessageRoleAssistant, Body: "older", CreatedAt: timestamp, Sources: []byte("null")},
	}, nil)

	got, next, err := (&PostgresRepository{queries: queries}).ListByConversation(ctx, 21, 0, "50")
	want := []entity.Message{
		{ID: 48, ConversationID: 21, Role: entity.MessageRoleUser, Text: "question", CreatedAt: createdAt},
		{
			ID: 49, ConversationID: 21, Role: entity.MessageRoleAssistant, Text: "guide", CreatedAt: createdAt,
			Sources: []entity.Source{{DocumentID: "doc-1", Title: "Guide", URL: "https://example.com/guide"}},
			Proposal: &entity.Action{
				ID: 42, MessageID: 49, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read",
				AccessRequest: &entity.AccessRequest{TaskID: "task-1", ExternalRequestID: "request-1", Status: "approved"},
			},
		},
	}
	if err != nil || next != "48" || !reflect.DeepEqual(got, want) {
		t.Fatalf("ListByConversation() = %+v, %q, %v, want %+v, %q, nil", got, next, err, want, "48")
	}
}

func TestListByConversationRejectsInvalidCursor(t *testing.T) {
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	repo := &PostgresRepository{queries: queries}
	for _, token := range []string{"bad", "0", "-1"} {
		got, next, err := repo.ListByConversation(t.Context(), 21, 2, token)
		if got != nil || next != "" || err != nil {
			t.Fatalf("ListByConversation() with cursor %q = %+v, %q, %v, want empty history", token, got, next, err)
		}
	}
}

func TestListByConversationPropagatesQueryError(t *testing.T) {
	ctx := t.Context()
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	dbErr := errors.New("database unavailable")
	queries.EXPECT().ListByConversation(ctx, sqlc.ListByConversationParams{
		ConversationID: 21, PageLimit: 3,
	}).Return(nil, dbErr)

	got, next, err := (&PostgresRepository{queries: queries}).ListByConversation(ctx, 21, 2, "")
	if got != nil || next != "" || !errors.Is(err, dbErr) {
		t.Fatalf("ListByConversation() = %+v, %q, %v, want nil, empty cursor, %v", got, next, err, dbErr)
	}
}

func TestGetOrCreateQuestion(t *testing.T) {
	ctx := t.Context()
	createdAt := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	queries.EXPECT().GetOrCreateQuestion(ctx, sqlc.GetOrCreateQuestionParams{
		ConversationID: 21, Body: "question", CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
		IdempotencyKey: 1001,
	}).Return(sqlc.GetOrCreateQuestionRow{ID: 48, Body: "question"}, nil)

	id, savedText, err := (&PostgresRepository{queries: queries}).GetOrCreateQuestion(ctx, entity.Message{
		ConversationID: 21, Role: entity.MessageRoleUser, Text: "question", CreatedAt: createdAt,
	}, 1001)
	if err != nil || id != 48 || savedText != "question" {
		t.Fatalf("GetOrCreateQuestion() = %d, %q, %v, want 48, %q, nil", id, savedText, err, "question")
	}
}

func TestClaimReply(t *testing.T) {
	ctx := t.Context()
	lease := time.Minute
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	claimUntil := time.Date(2026, 1, 5, 13, 0, 0, 0, time.UTC)
	queries.EXPECT().ClaimReply(ctx, sqlc.ClaimReplyParams{
		LeaseDuration: pgtype.Interval{Microseconds: lease.Microseconds(), Valid: true},
		QuestionID:    48,
	}).Return(pgtype.Timestamptz{Time: claimUntil, Valid: true}, nil)

	got, err := (&PostgresRepository{queries: queries}).ClaimReply(ctx, 48, lease)
	if err != nil || !got.Equal(claimUntil) {
		t.Fatalf("ClaimReply() = %v, %v, want %v, nil", got, err, claimUntil)
	}

	// A busy question returns no claim: the reply already exists.
	queries.EXPECT().ClaimReply(ctx, sqlc.ClaimReplyParams{
		LeaseDuration: pgtype.Interval{Microseconds: lease.Microseconds(), Valid: true},
		QuestionID:    48,
	}).Return(pgtype.Timestamptz{}, pgx.ErrNoRows)

	got, err = (&PostgresRepository{queries: queries}).ClaimReply(ctx, 48, lease)
	if err != nil || !got.IsZero() {
		t.Fatalf("ClaimReply() on busy question = %v, %v, want zero time, nil", got, err)
	}
}

func TestFinishReply(t *testing.T) {
	ctx := t.Context()
	claimUntil := time.Date(2026, 1, 5, 13, 0, 0, 0, time.UTC)
	claim := pgtype.Timestamptz{Time: claimUntil, Valid: true}
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	queries.EXPECT().FinishReply(ctx, sqlc.FinishReplyParams{
		AssistantMessageID: pgtype.Int8{Int64: 49, Valid: true},
		QuestionID:         48, ClaimUntil: claim,
	}).Return(int64(1), nil)

	if err := (&PostgresRepository{queries: queries}).FinishReply(ctx, 48, claimUntil, 49); err != nil {
		t.Fatalf("FinishReply() = %v, want nil", err)
	}

	// An expired claim cannot finish the reply.
	queries.EXPECT().FinishReply(ctx, sqlc.FinishReplyParams{
		AssistantMessageID: pgtype.Int8{Int64: 49, Valid: true},
		QuestionID:         48, ClaimUntil: claim,
	}).Return(int64(0), nil)

	if err := (&PostgresRepository{queries: queries}).FinishReply(ctx, 48, claimUntil, 49); !errors.Is(err, chaterrors.ErrReplyInProgress) {
		t.Fatalf("FinishReply() on expired claim = %v, want %v", err, chaterrors.ErrReplyInProgress)
	}
}

func TestGetReply(t *testing.T) {
	ctx := t.Context()
	createdAt := time.Date(2026, 1, 5, 12, 30, 0, 0, time.UTC)
	timestamp := pgtype.Timestamptz{Time: createdAt, Valid: true}
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	queries.EXPECT().GetReply(ctx, int64(48)).Return(sqlc.GetReplyRow{
		ID: 49, ConversationID: 21, Role: sqlc.ChatMessageRoleAssistant, Body: "guide", CreatedAt: timestamp,
		Sources:    []byte(`[{"DocumentID":"doc-1","Title":"Guide","URL":"https://example.com/guide"}]`),
		ProposalID: 42, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read",
	}, nil)

	reply, err := (&PostgresRepository{queries: queries}).GetReply(ctx, 48)
	want := &entity.Message{
		ID: 49, ConversationID: 21, Role: entity.MessageRoleAssistant, Text: "guide", CreatedAt: createdAt,
		Sources: []entity.Source{{DocumentID: "doc-1", Title: "Guide", URL: "https://example.com/guide"}},
		Proposal: &entity.Action{
			ID: 42, MessageID: 49, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read",
		},
	}
	if err != nil || !reflect.DeepEqual(reply, want) {
		t.Fatalf("GetReply() = %+v, %v, want %+v, nil", reply, err, want)
	}

	// A question without a saved reply returns nil without an error.
	queries.EXPECT().GetReply(ctx, int64(48)).Return(sqlc.GetReplyRow{}, pgx.ErrNoRows)
	reply, err = (&PostgresRepository{queries: queries}).GetReply(ctx, 48)
	if reply != nil || err != nil {
		t.Fatalf("GetReply() without a reply = %+v, %v, want nil, nil", reply, err)
	}
}

func TestUnreadableSourcesDoNotBecomeAnEmptyChatHistory(t *testing.T) {
	ctx := t.Context()
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	queries.EXPECT().ListByConversation(ctx, sqlc.ListByConversationParams{
		ConversationID: 21, PageLimit: 3,
	}).Return([]sqlc.ListByConversationRow{
		{ID: 48, ConversationID: 21, Role: sqlc.ChatMessageRoleAssistant, Sources: []byte(`{"unexpected":"object"}`)},
	}, nil)

	got, next, err := (&PostgresRepository{queries: queries}).ListByConversation(ctx, 21, 2, "")
	var shapeError *json.UnmarshalTypeError
	if got != nil || next != "" || !errors.As(err, &shapeError) {
		t.Fatalf("ListByConversation() = %+v, %q, %v, want a sources decode error", got, next, err)
	}
}
