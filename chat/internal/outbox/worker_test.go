package outbox

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"avito-ai-buddy/chat/internal/config"
	"avito-ai-buddy/chat/internal/entity"

	"go.uber.org/zap"
)

type dispatchQueue struct {
	fetch     func() ([]entity.OutboxMessage, error)
	processed func([]string) error
	retryable func([]string) error
}

func (q dispatchQueue) GetMessages(context.Context, int32, time.Duration) ([]entity.OutboxMessage, error) {
	return q.fetch()
}

func (q dispatchQueue) MarkAsProcessed(_ context.Context, keys []string) error {
	return q.processed(keys)
}

func (q dispatchQueue) MarkAsRetryable(_ context.Context, keys []string) error {
	return q.retryable(keys)
}

type dispatchTx func(context.Context, func(context.Context) error) error

func (tx dispatchTx) WithTx(ctx context.Context, fn func(context.Context) error) error {
	return tx(ctx, fn)
}

func TestAccessRequestIsRetriedAfterPublishTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		const kind = entity.OutboxKindCreateAccessRequest
		initial := []entity.OutboxMessage{
			{IdempotencyKey: "42", Kind: kind, Data: []byte("first request")},
			{IdempotencyKey: "43", Kind: kind, Data: []byte("second request")},
			{IdempotencyKey: "44", Kind: 999, Data: []byte("unsupported request")},
		}
		var publishAttempts []string
		var processed, retried [][]string
		started := time.Now()
		var fetchTimes []time.Duration
		queue := dispatchQueue{
			fetch: func() ([]entity.OutboxMessage, error) {
				fetchTimes = append(fetchTimes, time.Since(started))
				if len(fetchTimes) == 1 {
					return initial, nil
				}
				return initial[:1], nil
			},
			processed: func(keys []string) error {
				processed = append(processed, keys)
				return nil
			},
			retryable: func(keys []string) error {
				retried = append(retried, keys)
				if len(retried) == 2 {
					cancel()
				}
				return nil
			},
		}
		cfg := &config.Config{}
		cfg.Outbox.RetryDelay = 5 * time.Second
		cfg.Outbox.PublishTimeout = 2 * time.Second
		tx := dispatchTx(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
		worker := New(zap.NewNop(), queue, func(gotKind int32) (KindHandler, error) {
			if gotKind != kind {
				return nil, errors.New("unsupported action")
			}
			return func(ctx context.Context, data []byte) error {
				publishAttempts = append(publishAttempts, string(data))
				if len(publishAttempts) == 1 {
					<-ctx.Done()
					return ctx.Err()
				}
				return nil
			}, nil
		}, cfg, tx)

		worker.Start(ctx, 1, 10, time.Second, time.Minute)

		wantAttempts := []string{"first request", "second request", "first request"}
		if !reflect.DeepEqual(publishAttempts, wantAttempts) {
			t.Fatalf("publish attempts = %v, want %v", publishAttempts, wantAttempts)
		}
		wantProcessed := [][]string{{"43"}, {"42"}}
		wantRetried := [][]string{{"42"}, {}}
		if !reflect.DeepEqual(processed, wantProcessed) || !reflect.DeepEqual(retried, wantRetried) {
			t.Fatalf("processed = %v and retryable = %v, want %v and %v", processed, retried, wantProcessed, wantRetried)
		}
		if want := []time.Duration{time.Second, 8 * time.Second}; !reflect.DeepEqual(fetchTimes, want) {
			t.Fatalf("polls at %v, want %v", fetchTimes, want)
		}
	})
}

func TestDispatchResumesAfterStorageFailure(t *testing.T) {
	storageErr := errors.New("database unavailable")
	for _, stage := range []string{"fetch", "mark processed", "mark retryable"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				attempts := 0
				message := entity.OutboxMessage{IdempotencyKey: "42", Kind: entity.OutboxKindCreateAccessRequest}
				queue := dispatchQueue{
					fetch: func() ([]entity.OutboxMessage, error) {
						attempts++
						if attempts == 1 && stage == "fetch" {
							return nil, storageErr
						}
						return []entity.OutboxMessage{message}, nil
					},
					processed: func(keys []string) error {
						if !reflect.DeepEqual(keys, []string{"42"}) {
							t.Errorf("processed keys = %v, want [42]", keys)
						}
						if attempts == 1 && stage == "mark processed" {
							return storageErr
						}
						return nil
					},
					retryable: func(keys []string) error {
						if len(keys) != 0 {
							t.Errorf("successful delivery requested retry: %v", keys)
						}
						if attempts == 1 && stage == "mark retryable" {
							return storageErr
						}
						return nil
					},
				}
				var results []error
				tx := dispatchTx(func(ctx context.Context, fn func(context.Context) error) error {
					err := fn(ctx)
					results = append(results, err)
					if len(results) == 2 {
						cancel()
					}
					return err
				})
				cfg := &config.Config{}
				cfg.Outbox.PublishTimeout = time.Second
				worker := New(zap.NewNop(), queue, func(int32) (KindHandler, error) {
					return func(context.Context, []byte) error { return nil }, nil
				}, cfg, tx)

				worker.Start(ctx, 1, 10, time.Second, time.Minute)

				if len(results) != 2 || !errors.Is(results[0], storageErr) || results[1] != nil {
					t.Fatalf("dispatch transactions = %v, want storage failure followed by success", results)
				}
			})
		})
	}
}
