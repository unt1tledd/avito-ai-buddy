package inbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	sqlc "avito-ai-buddy/chat/internal/repository/inbox/sqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type fakeQuerier struct {
	tryInsert func(context.Context, sqlc.TryInsertInboxMessageParams) (string, error)
	remove    func(context.Context, string) error
	cleanup   func(context.Context, pgtype.Interval) error
}

func (q *fakeQuerier) TryInsertInboxMessage(ctx context.Context, params sqlc.TryInsertInboxMessageParams) (string, error) {
	return q.tryInsert(ctx, params)
}

func (q *fakeQuerier) DeleteInboxMessage(ctx context.Context, idempotencyKey string) error {
	return q.remove(ctx, idempotencyKey)
}

func (q *fakeQuerier) CleanupInboxMessages(ctx context.Context, retention pgtype.Interval) error {
	return q.cleanup(ctx, retention)
}

func TestTryInsertReportsFirstDelivery(t *testing.T) {
	ctx := t.Context()
	queries := &fakeQuerier{
		tryInsert: func(_ context.Context, params sqlc.TryInsertInboxMessageParams) (string, error) {
			if params.IdempotencyKey != "access-42" || params.Kind != 1 {
				t.Errorf("unexpected params: %+v", params)
			}
			return params.IdempotencyKey, nil
		},
	}

	inserted, err := (&PostgresRepository{queries: queries}).TryInsert(ctx, entity.InboxMessage{
		IdempotencyKey: "access-42", Kind: 1, Data: []byte(`{"task_id":"task-42"}`),
	})
	if err != nil || !inserted {
		t.Fatalf("TryInsert() = %v, %v, want true, nil", inserted, err)
	}
}

func TestTryInsertSkipsKnownKeys(t *testing.T) {
	ctx := t.Context()
	queries := &fakeQuerier{
		tryInsert: func(context.Context, sqlc.TryInsertInboxMessageParams) (string, error) {
			return "", pgx.ErrNoRows
		},
	}

	inserted, err := (&PostgresRepository{queries: queries}).TryInsert(ctx, entity.InboxMessage{
		IdempotencyKey: "access-42", Kind: 1, Data: []byte(`{}`),
	})
	if err != nil || inserted {
		t.Fatalf("TryInsert() = %v, %v, want false, nil", inserted, err)
	}
}

func TestTryInsertPropagatesDatabaseFailure(t *testing.T) {
	ctx := t.Context()
	dbErr := errors.New("database unavailable")
	queries := &fakeQuerier{
		tryInsert: func(context.Context, sqlc.TryInsertInboxMessageParams) (string, error) {
			return "", dbErr
		},
	}

	inserted, err := (&PostgresRepository{queries: queries}).TryInsert(ctx, entity.InboxMessage{IdempotencyKey: "42"})
	if inserted || !errors.Is(err, dbErr) {
		t.Fatalf("TryInsert() = %v, %v, want false, %v", inserted, err, dbErr)
	}
}

func TestDeleteInboxMessage(t *testing.T) {
	ctx := t.Context()
	dbErr := errors.New("database unavailable")
	var got string
	queries := &fakeQuerier{
		remove: func(_ context.Context, key string) error {
			got = key
			return dbErr
		},
	}

	if err := (&PostgresRepository{queries: queries}).Delete(ctx, "access-42"); !errors.Is(err, dbErr) {
		t.Fatalf("Delete() = %v, want %v", err, dbErr)
	}
	if got != "access-42" {
		t.Fatalf("Delete() key = %q, want access-42", got)
	}
}

func TestCleanupOlderThan(t *testing.T) {
	ctx := t.Context()
	var got pgtype.Interval
	queries := &fakeQuerier{
		cleanup: func(_ context.Context, retention pgtype.Interval) error {
			got = retention
			return nil
		},
	}

	retention := 24 * time.Hour
	if err := (&PostgresRepository{queries: queries}).CleanupOlderThan(ctx, retention); err != nil {
		t.Fatalf("CleanupOlderThan() = %v, want nil", err)
	}
	if got.Microseconds != retention.Microseconds() || !got.Valid {
		t.Fatalf("retention interval = %+v, want %d microseconds", got, retention.Microseconds())
	}
}
