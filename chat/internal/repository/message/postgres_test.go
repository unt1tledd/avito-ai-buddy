package message

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	sqlc "avito-ai-buddy/chat/internal/repository/message/sqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// fakeQuerier implements sqlc.Querier with call expectations defined per test.
type fakeQuerier struct {
	claimReply          func(context.Context, sqlc.ClaimReplyParams) (pgtype.Timestamptz, error)
	create              func(context.Context, sqlc.CreateParams) (int64, error)
	createIdempotent    func(context.Context, sqlc.CreateIdempotentMessageParams) (int64, error)
	finishReply         func(context.Context, sqlc.FinishReplyParams) (int64, error)
	getOrCreateQuestion func(context.Context, sqlc.GetOrCreateQuestionParams) (sqlc.GetOrCreateQuestionRow, error)
	getReply            func(context.Context, int64) (sqlc.GetReplyRow, error)
	listByConversation  func(context.Context, sqlc.ListByConversationParams) ([]sqlc.ListByConversationRow, error)
}

func (q *fakeQuerier) ClaimReply(ctx context.Context, arg sqlc.ClaimReplyParams) (pgtype.Timestamptz, error) {
	return q.claimReply(ctx, arg)
}

func (q *fakeQuerier) Create(ctx context.Context, arg sqlc.CreateParams) (int64, error) {
	return q.create(ctx, arg)
}

func (q *fakeQuerier) CreateIdempotentMessage(ctx context.Context, arg sqlc.CreateIdempotentMessageParams) (int64, error) {
	return q.createIdempotent(ctx, arg)
}

func (q *fakeQuerier) FinishReply(ctx context.Context, arg sqlc.FinishReplyParams) (int64, error) {
	return q.finishReply(ctx, arg)
}

func (q *fakeQuerier) GetOrCreateQuestion(ctx context.Context, arg sqlc.GetOrCreateQuestionParams) (sqlc.GetOrCreateQuestionRow, error) {
	return q.getOrCreateQuestion(ctx, arg)
}

func (q *fakeQuerier) GetReply(ctx context.Context, id int64) (sqlc.GetReplyRow, error) {
	return q.getReply(ctx, id)
}

func (q *fakeQuerier) ListByConversation(ctx context.Context, arg sqlc.ListByConversationParams) ([]sqlc.ListByConversationRow, error) {
	return q.listByConversation(ctx, arg)
}

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
			queries := &fakeQuerier{
				create: func(_ context.Context, params sqlc.CreateParams) (int64, error) {
					want := sqlc.CreateParams{
						ConversationID: 21, Role: tc.wantRole, Body: "guide", Sources: []byte(tc.wantSources),
						CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
					}
					if !reflect.DeepEqual(params, want) {
						t.Errorf("Create() params = %+v, want %+v", params, want)
					}
					return 42, nil
				},
			}

			got, err := (&PostgresRepository{queries: queries}).Create(ctx, entity.Message{
				ConversationID: 21, Role: tc.role, Text: "guide", Sources: tc.sources, CreatedAt: createdAt,
			})
			if err != nil || got != 42 {
				t.Fatalf("Create() = %d, %v, want 42, nil", got, err)
			}
		})
	}
}

func TestCreateIdempotent(t *testing.T) {
	ctx := t.Context()
	createdAt := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	message := entity.Message{
		ConversationID: 21, Role: entity.MessageRoleAssistant, Text: "status update", CreatedAt: createdAt,
	}
	queries := &fakeQuerier{
		createIdempotent: func(_ context.Context, params sqlc.CreateIdempotentMessageParams) (int64, error) {
			want := sqlc.CreateIdempotentMessageParams{
				ConversationID: 21, Role: sqlc.ChatMessageRoleAssistant, Body: "status update",
				Sources: []byte("null"), CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
				IdempotencyKey: 777,
			}
			if !reflect.DeepEqual(params, want) {
				t.Errorf("CreateIdempotentMessage() params = %+v, want %+v", params, want)
			}
			return 49, nil
		},
	}

	created, err := (&PostgresRepository{queries: queries}).CreateIdempotent(ctx, message, 777)
	if err != nil || !created {
		t.Fatalf("CreateIdempotent() = %v, %v, want true, nil", created, err)
	}

	// A known idempotency key does not create a duplicate message.
	queries.createIdempotent = func(context.Context, sqlc.CreateIdempotentMessageParams) (int64, error) {
		return 0, pgx.ErrNoRows
	}
	created, err = (&PostgresRepository{queries: queries}).CreateIdempotent(ctx, message, 777)
	if err != nil || created {
		t.Fatalf("CreateIdempotent() on known key = %v, %v, want false, nil", created, err)
	}
}

func TestGetOrCreateQuestion(t *testing.T) {
	ctx := t.Context()
	createdAt := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	queries := &fakeQuerier{
		getOrCreateQuestion: func(_ context.Context, params sqlc.GetOrCreateQuestionParams) (sqlc.GetOrCreateQuestionRow, error) {
			want := sqlc.GetOrCreateQuestionParams{
				ConversationID: 21, Body: "question", CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
				IdempotencyKey: 1001,
			}
			if !reflect.DeepEqual(params, want) {
				t.Errorf("GetOrCreateQuestion() params = %+v, want %+v", params, want)
			}
			return sqlc.GetOrCreateQuestionRow{ID: 48, Body: "question"}, nil
		},
	}

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
	claimUntil := time.Date(2026, 1, 5, 13, 0, 0, 0, time.UTC)
	queries := &fakeQuerier{
		claimReply: func(_ context.Context, params sqlc.ClaimReplyParams) (pgtype.Timestamptz, error) {
			want := sqlc.ClaimReplyParams{
				LeaseDuration: pgtype.Interval{Microseconds: lease.Microseconds(), Valid: true},
				QuestionID:    48,
			}
			if !reflect.DeepEqual(params, want) {
				t.Errorf("ClaimReply() params = %+v, want %+v", params, want)
			}
			return pgtype.Timestamptz{Time: claimUntil, Valid: true}, nil
		},
	}

	got, err := (&PostgresRepository{queries: queries}).ClaimReply(ctx, 48, lease)
	if err != nil || !got.Equal(claimUntil) {
		t.Fatalf("ClaimReply() = %v, %v, want %v, nil", got, err, claimUntil)
	}

	// A busy question returns no claim: the reply already exists.
	queries.claimReply = func(context.Context, sqlc.ClaimReplyParams) (pgtype.Timestamptz, error) {
		return pgtype.Timestamptz{}, pgx.ErrNoRows
	}
	got, err = (&PostgresRepository{queries: queries}).ClaimReply(ctx, 48, lease)
	if err != nil || !got.IsZero() {
		t.Fatalf("ClaimReply() on busy question = %v, %v, want zero time, nil", got, err)
	}
}

func TestFinishReply(t *testing.T) {
	ctx := t.Context()
	claimUntil := time.Date(2026, 1, 5, 13, 0, 0, 0, time.UTC)
	claim := pgtype.Timestamptz{Time: claimUntil, Valid: true}
	queries := &fakeQuerier{
		finishReply: func(_ context.Context, params sqlc.FinishReplyParams) (int64, error) {
			want := sqlc.FinishReplyParams{
				AssistantMessageID: pgtype.Int8{Int64: 49, Valid: true},
				QuestionID:         48, ClaimUntil: claim,
			}
			if !reflect.DeepEqual(params, want) {
				t.Errorf("FinishReply() params = %+v, want %+v", params, want)
			}
			return 1, nil
		},
	}

	if err := (&PostgresRepository{queries: queries}).FinishReply(ctx, 48, claimUntil, 49); err != nil {
		t.Fatalf("FinishReply() = %v, want nil", err)
	}

	// An expired claim cannot finish the reply.
	queries.finishReply = func(context.Context, sqlc.FinishReplyParams) (int64, error) { return 0, nil }
	if err := (&PostgresRepository{queries: queries}).FinishReply(ctx, 48, claimUntil, 49); !errors.Is(err, chaterrors.ErrReplyInProgress) {
		t.Fatalf("FinishReply() on expired claim = %v, want %v", err, chaterrors.ErrReplyInProgress)
	}
}

func TestGetReply(t *testing.T) {
	ctx := t.Context()
	createdAt := time.Date(2026, 1, 5, 12, 30, 0, 0, time.UTC)
	timestamp := pgtype.Timestamptz{Time: createdAt, Valid: true}
	queries := &fakeQuerier{
		getReply: func(_ context.Context, id int64) (sqlc.GetReplyRow, error) {
			if id != 48 {
				t.Errorf("GetReply() id = %d, want 48", id)
			}
			return sqlc.GetReplyRow{
				ID: 49, ConversationID: 21, Role: sqlc.ChatMessageRoleAssistant, Body: "guide", CreatedAt: timestamp,
				Sources:    []byte(`[{"DocumentID":"doc-1","Title":"Guide","URL":"https://example.com/guide"}]`),
				ProposalID: 42, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read",
			}, nil
		},
	}

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
	queries.getReply = func(context.Context, int64) (sqlc.GetReplyRow, error) {
		return sqlc.GetReplyRow{}, pgx.ErrNoRows
	}
	reply, err = (&PostgresRepository{queries: queries}).GetReply(ctx, 48)
	if reply != nil || err != nil {
		t.Fatalf("GetReply() without a reply = %+v, %v, want nil, nil", reply, err)
	}
}

func TestListByConversation(t *testing.T) {
	ctx := WithHistoryPageSize(t.Context(), 2)
	createdAt := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	timestamp := pgtype.Timestamptz{Time: createdAt, Valid: true}
	queries := &fakeQuerier{
		listByConversation: func(_ context.Context, params sqlc.ListByConversationParams) ([]sqlc.ListByConversationRow, error) {
			want := sqlc.ListByConversationParams{ConversationID: 21, BeforeID: 50, PageLimit: 3}
			if !reflect.DeepEqual(params, want) {
				t.Errorf("ListByConversation() params = %+v, want %+v", params, want)
			}
			return []sqlc.ListByConversationRow{
				{
					ID: 49, ConversationID: 21, Role: sqlc.ChatMessageRoleAssistant, Body: "guide", CreatedAt: timestamp,
					Sources:    []byte(`[{"DocumentID":"doc-1","Title":"Guide","URL":"https://example.com/guide"}]`),
					ProposalID: 42, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read",
					TaskID: "task-1", ExternalRequestID: "request-1", Status: "approved",
				},
				{ID: 48, ConversationID: 21, Role: sqlc.ChatMessageRoleUser, Body: "question", CreatedAt: timestamp, Sources: []byte("null")},
				{ID: 47, ConversationID: 21, Role: sqlc.ChatMessageRoleAssistant, Body: "older", CreatedAt: timestamp, Sources: []byte("null")},
			}, nil
		},
	}

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
	queries := &fakeQuerier{
		listByConversation: func(context.Context, sqlc.ListByConversationParams) ([]sqlc.ListByConversationRow, error) {
			t.Error("ListByConversation() query executed with an invalid cursor")
			return nil, nil
		},
	}
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
	dbErr := errors.New("database unavailable")
	queries := &fakeQuerier{
		listByConversation: func(context.Context, sqlc.ListByConversationParams) ([]sqlc.ListByConversationRow, error) {
			return nil, dbErr
		},
	}

	got, next, err := (&PostgresRepository{queries: queries}).ListByConversation(ctx, 21, 2, "")
	if got != nil || next != "" || !errors.Is(err, dbErr) {
		t.Fatalf("ListByConversation() = %+v, %q, %v, want nil, empty cursor, %v", got, next, err, dbErr)
	}
}

func TestUnreadableSourcesDoNotBecomeAnEmptyChatHistory(t *testing.T) {
	ctx := t.Context()
	queries := &fakeQuerier{
		listByConversation: func(context.Context, sqlc.ListByConversationParams) ([]sqlc.ListByConversationRow, error) {
			return []sqlc.ListByConversationRow{
				{ID: 48, ConversationID: 21, Role: sqlc.ChatMessageRoleAssistant, Sources: []byte(`{"unexpected":"object"}`)},
			}, nil
		},
	}

	got, next, err := (&PostgresRepository{queries: queries}).ListByConversation(ctx, 21, 2, "")
	if got != nil || next != "" || err == nil {
		t.Fatalf("ListByConversation() = %+v, %q, %v, want unreadable sources error", got, next, err)
	}
}
