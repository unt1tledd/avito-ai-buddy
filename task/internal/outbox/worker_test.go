package outbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"avito-ai-buddy/task/internal/config"
	"avito-ai-buddy/task/internal/entity"
)

type txStub func(context.Context, func(context.Context) error) error

func (f txStub) WithTx(ctx context.Context, run func(context.Context) error) error {
	return f(ctx, run)
}

type publisherStub func(context.Context, int64, []byte) error

func (f publisherStub) PublishTaskStatus(ctx context.Context, id int64, data []byte) error {
	return f(ctx, id, data)
}

type queueStub struct {
	get       func(context.Context, int32) ([]entity.OutboxMessage, error)
	processed func(context.Context, []int64) error
	retryable func(context.Context, []int64, time.Duration) error
	failed    func(context.Context, []int64, time.Duration) error
}

func (q queueStub) GetMessages(ctx context.Context, size int32) ([]entity.OutboxMessage, error) {
	return q.get(ctx, size)
}

func (q queueStub) MarkAsProcessed(ctx context.Context, ids []int64) error {
	return q.processed(ctx, ids)
}

func (q queueStub) MarkAsRetryable(ctx context.Context, ids []int64, delay time.Duration) error {
	return q.retryable(ctx, ids, delay)
}

func (q queueStub) MarkAsFailed(ctx context.Context, ids []int64, delay time.Duration) error {
	return q.failed(ctx, ids, delay)
}

func TestDispatchClassifiesDeliveryResults(t *testing.T) {
	type txKey struct{}
	ctx := t.Context()
	const marker = "outbox transaction"
	cfg := config.OutboxConfig{BatchSize: 5, MaxAttempts: 3, RetryDelay: 3 * time.Second,
		FailedRetryDelay: time.Minute, PublishTimeout: 5 * time.Second}
	var processed, retryable, failed, published []int64

	checkTx := func(ctx context.Context) {
		t.Helper()
		if ctx.Value(txKey{}) != marker {
			t.Fatal("operation ran outside the outbox transaction")
		}
	}
	queue := queueStub{
		get: func(ctx context.Context, limit int32) ([]entity.OutboxMessage, error) {
			checkTx(ctx)
			if limit != cfg.BatchSize {
				t.Fatalf("batch size=%d, want %d", limit, cfg.BatchSize)
			}
			return []entity.OutboxMessage{
				{TaskID: 1},
				{TaskID: 2},
				{TaskID: 3, Attempts: 2},
				{TaskID: 4, Attempts: 3, Failed: true},
				{TaskID: 5, Attempts: 1, Failed: true},
			}, nil
		},
		processed: func(ctx context.Context, ids []int64) error {
			checkTx(ctx)
			processed = append(processed, ids...)
			return nil
		},
		retryable: func(ctx context.Context, ids []int64, delay time.Duration) error {
			checkTx(ctx)
			if delay != cfg.RetryDelay {
				t.Fatalf("retry delay=%s, want %s", delay, cfg.RetryDelay)
			}
			retryable = append(retryable, ids...)
			return nil
		},
		failed: func(ctx context.Context, ids []int64, delay time.Duration) error {
			checkTx(ctx)
			if delay != cfg.FailedRetryDelay {
				t.Fatalf("failed retry delay=%s, want %s", delay, cfg.FailedRetryDelay)
			}
			failed = append(failed, ids...)
			return nil
		},
	}
	tx := txStub(func(ctx context.Context, run func(context.Context) error) error {
		return run(context.WithValue(ctx, txKey{}, marker))
	})
	publisher := publisherStub(func(ctx context.Context, id int64, _ []byte) error {
		checkTx(ctx)
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("publish context has no timeout")
		}
		published = append(published, id)
		if id == 1 || id == 4 {
			return nil
		}
		return errors.New("Kafka unavailable")
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := New(logger, queue, tx, publisher, cfg).dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(published, []int64{1, 2, 3, 4, 5}) ||
		!slices.Equal(processed, []int64{1, 4}) ||
		!slices.Equal(retryable, []int64{2}) ||
		!slices.Equal(failed, []int64{3, 5}) {
		t.Fatalf("published=%v, processed=%v, retryable=%v, failed=%v", published, processed, retryable, failed)
	}
}

func TestDispatchCancellationDoesNotMarkDelivery(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	queue := queueStub{get: func(context.Context, int32) ([]entity.OutboxMessage, error) {
		return []entity.OutboxMessage{{TaskID: 1}}, nil
	}}
	tx := txStub(func(ctx context.Context, run func(context.Context) error) error {
		return run(ctx)
	})
	publisher := publisherStub(func(context.Context, int64, []byte) error {
		cancel()
		return context.Canceled
	})
	worker := New(slog.Default(), queue, tx, publisher, config.OutboxConfig{PublishTimeout: time.Second})
	if err := worker.dispatch(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatch err=%v, want canceled", err)
	}
}
