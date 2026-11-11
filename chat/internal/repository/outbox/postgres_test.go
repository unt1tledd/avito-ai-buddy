package outbox

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	sqlc "avito-ai-buddy/chat/internal/repository/outbox/sqlc"
)

// fakeQuerier implements sqlc.Querier with call expectations defined per test.
type fakeQuerier struct {
	sendOutboxMessage func(context.Context, sqlc.SendOutboxMessageParams) error
	getOutboxMessage  func(context.Context, string) (sqlc.GetOutboxMessageRow, error)
	getOutboxMessages func(context.Context, sqlc.GetOutboxMessagesParams) ([]sqlc.GetOutboxMessagesRow, error)
	markAsProcessed   func(context.Context, []string) error
	markAsRetryable   func(context.Context, []string) error
	markAsFailed      func(context.Context, []string) error
	resetMessage      func(context.Context, string) error
}

func (q *fakeQuerier) SendOutboxMessage(ctx context.Context, params sqlc.SendOutboxMessageParams) error {
	return q.sendOutboxMessage(ctx, params)
}

func (q *fakeQuerier) GetOutboxMessage(ctx context.Context, idempotencyKey string) (sqlc.GetOutboxMessageRow, error) {
	return q.getOutboxMessage(ctx, idempotencyKey)
}

func (q *fakeQuerier) ResetOutboxMessage(ctx context.Context, idempotencyKey string) error {
	return q.resetMessage(ctx, idempotencyKey)
}

func (q *fakeQuerier) GetOutboxMessages(ctx context.Context, params sqlc.GetOutboxMessagesParams) ([]sqlc.GetOutboxMessagesRow, error) {
	return q.getOutboxMessages(ctx, params)
}

func (q *fakeQuerier) MarkOutboxMessagesAsProcessed(ctx context.Context, keys []string) error {
	return q.markAsProcessed(ctx, keys)
}

func (q *fakeQuerier) MarkOutboxMessagesAsRetryable(ctx context.Context, keys []string) error {
	return q.markAsRetryable(ctx, keys)
}

func (q *fakeQuerier) MarkOutboxMessagesAsFailed(ctx context.Context, keys []string) error {
	return q.markAsFailed(ctx, keys)
}

func TestSendOutboxMessage(t *testing.T) {
	ctx := t.Context()
	queries := &fakeQuerier{
		sendOutboxMessage: func(_ context.Context, params sqlc.SendOutboxMessageParams) error {
			if params.IdempotencyKey != "42" || params.Kind != entity.OutboxKindCreateAccessRequest {
				t.Errorf("unexpected params: %+v", params)
			}
			return nil
		},
	}
	message := entity.OutboxMessage{
		IdempotencyKey: "42", Kind: entity.OutboxKindCreateAccessRequest, Data: []byte(`{"proposal_id":42}`),
	}

	if err := (&PostgresRepository{queries: queries}).SendOutboxMessage(ctx, message); err != nil {
		t.Fatalf("SendOutboxMessage() = %v, want nil", err)
	}
}

func TestGetMessages(t *testing.T) {
	ttl := 1500 * time.Millisecond
	ctx := WithFetchOptions(t.Context(), FetchOptions{BatchSize: 3, InProgressTTL: ttl})
	queries := &fakeQuerier{
		getOutboxMessages: func(_ context.Context, params sqlc.GetOutboxMessagesParams) ([]sqlc.GetOutboxMessagesRow, error) {
			if params.BatchSize != 3 || params.InProgressTtl.Microseconds != 1500000 {
				t.Errorf("unexpected params: %+v", params)
			}
			return []sqlc.GetOutboxMessagesRow{
				{IdempotencyKey: "42", Kind: entity.OutboxKindCreateAccessRequest, Data: []byte(`{"proposal_id":42}`), Attempts: 1},
				{IdempotencyKey: "43", Kind: entity.OutboxKindCreateAccessRequest, Data: []byte(`{"proposal_id":43}`)},
			}, nil
		},
	}

	got, err := (&PostgresRepository{queries: queries}).GetMessages(ctx)
	want := []entity.OutboxMessage{
		{IdempotencyKey: "42", Kind: entity.OutboxKindCreateAccessRequest, Data: []byte(`{"proposal_id":42}`), Attempts: 1},
		{IdempotencyKey: "43", Kind: entity.OutboxKindCreateAccessRequest, Data: []byte(`{"proposal_id":43}`)},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("GetMessages() = %+v, %v, want %+v, nil", got, err, want)
	}
}

func TestGetMessagesRequiresFetchOptions(t *testing.T) {
	got, err := (&PostgresRepository{queries: &fakeQuerier{}}).GetMessages(t.Context())
	if got != nil || err == nil {
		t.Fatalf("GetMessages() = %+v, %v, want nil, error", got, err)
	}
}

func TestMarkAsProcessed(t *testing.T) {
	ctx := t.Context()
	queries := &fakeQuerier{
		markAsProcessed: func(_ context.Context, keys []string) error {
			if !reflect.DeepEqual(keys, []string{"42", "43"}) {
				t.Errorf("processed keys = %v, want [42 43]", keys)
			}
			return nil
		},
	}

	if err := (&PostgresRepository{queries: queries}).MarkAsProcessed(ctx, []string{"42", "43"}); err != nil {
		t.Fatalf("MarkAsProcessed() = %v, want nil", err)
	}
}

func TestMarkAsRetryable(t *testing.T) {
	ctx := t.Context()
	dbErr := errors.New("database unavailable")
	queries := &fakeQuerier{
		markAsRetryable: func(context.Context, []string) error { return dbErr },
	}

	if err := (&PostgresRepository{queries: queries}).MarkAsRetryable(ctx, []string{"42", "43"}); !errors.Is(err, dbErr) {
		t.Fatalf("MarkAsRetryable() = %v, want %v", err, dbErr)
	}
}

func TestMarkAsFailed(t *testing.T) {
	ctx := t.Context()
	queries := &fakeQuerier{
		markAsFailed: func(_ context.Context, keys []string) error {
			if !reflect.DeepEqual(keys, []string{"42"}) {
				t.Errorf("failed keys = %v, want [42]", keys)
			}
			return nil
		},
	}

	if err := (&PostgresRepository{queries: queries}).MarkAsFailed(ctx, []string{"42"}); err != nil {
		t.Fatalf("MarkAsFailed() = %v, want nil", err)
	}
}

func TestGetMessageStatusAndReset(t *testing.T) {
	ctx := t.Context()
	dbErr := errors.New("database unavailable")
	queries := &fakeQuerier{
		getOutboxMessage: func(context.Context, string) (sqlc.GetOutboxMessageRow, error) {
			return sqlc.GetOutboxMessageRow{Status: sqlc.ChatOutboxStatusFAILED, Attempts: 5}, nil
		},
		resetMessage: func(_ context.Context, key string) error {
			if key != "42" {
				t.Errorf("reset key = %q, want 42", key)
			}
			return dbErr
		},
	}
	repository := &PostgresRepository{queries: queries}

	status, attempts, err := repository.GetMessageStatus(ctx, "42")
	if err != nil || status != entity.OutboxStatusFailed || attempts != 5 {
		t.Fatalf("GetMessageStatus() = %q, %d, %v, want FAILED, 5, nil", status, attempts, err)
	}
	if err := repository.ResetMessage(ctx, "42"); !errors.Is(err, dbErr) {
		t.Fatalf("ResetMessage() = %v, want %v", err, dbErr)
	}
}

func TestOutboxReadFailureIsNotAnEmptyQueue(t *testing.T) {
	ctx := WithFetchOptions(t.Context(), FetchOptions{BatchSize: 10, InProgressTTL: time.Minute})
	dbErr := errors.New("database unavailable")
	queries := &fakeQuerier{
		getOutboxMessages: func(context.Context, sqlc.GetOutboxMessagesParams) ([]sqlc.GetOutboxMessagesRow, error) {
			return nil, dbErr
		},
	}

	got, err := (&PostgresRepository{queries: queries}).GetMessages(ctx)
	if got != nil || !errors.Is(err, dbErr) {
		t.Fatalf("GetMessages() = %+v, %v, want nil, %v", got, err, dbErr)
	}
}
