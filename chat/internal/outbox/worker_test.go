package outbox

import (
	"context"
	"errors"
	"fmt"
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
	failed    func([]string) error
}

func (q dispatchQueue) GetMessages(context.Context) ([]entity.OutboxMessage, error) {
	return q.fetch()
}

func (q dispatchQueue) MarkAsProcessed(_ context.Context, keys []string) error {
	return q.processed(keys)
}

func (q dispatchQueue) MarkAsRetryable(_ context.Context, keys []string) error {
	return q.retryable(keys)
}

func (q dispatchQueue) MarkAsFailed(_ context.Context, keys []string) error {
	return q.failed(keys)
}

type dispatchTx func(context.Context, func(context.Context) error) error

func (tx dispatchTx) WithTx(ctx context.Context, fn func(context.Context) error) error {
	return tx(ctx, fn)
}

type recordingFailures struct {
	calls []string
}

func (f *recordingFailures) PublishAccessRequestFailed(_ context.Context, userID string, proposalID int64, idempotencyKey string) error {
	f.calls = append(f.calls, fmt.Sprintf("%s|%d|%s", userID, proposalID, idempotencyKey))
	return nil
}

func outboxTestConfig(publishTimeout, retryDelay time.Duration, maxAttempts int32) *config.Config {
	cfg := &config.Config{}
	cfg.Outbox.BatchSize = 10
	cfg.Outbox.InProgressTTL = time.Minute
	cfg.Outbox.PollInterval = time.Second
	cfg.Outbox.RetryDelay = retryDelay
	cfg.Outbox.PublishTimeout = publishTimeout
	cfg.Outbox.MaxAttempts = maxAttempts
	return cfg
}

const accessRequestKind = entity.OutboxKindCreateAccessRequest

func TestAccessRequestIsRetriedAfterPublishTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		initial := []entity.OutboxMessage{
			{IdempotencyKey: "42", Kind: accessRequestKind, Data: []byte("first request")},
			{IdempotencyKey: "43", Kind: accessRequestKind, Data: []byte("second request")},
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
				if len(processed) == 2 {
					// Both messages succeeded, the test can stop the worker.
					cancel()
				}
				return nil
			},
			retryable: func(keys []string) error {
				retried = append(retried, keys)
				return nil
			},
			failed: func([]string) error { return nil },
		}
		cfg := outboxTestConfig(2*time.Second, 5*time.Second, 3)
		tx := dispatchTx(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
		worker := New(zap.NewNop(), queue, func(gotKind int32) (KindHandler, error) {
			if gotKind != accessRequestKind {
				// An unsupported kind behaves like a failed delivery.
				return func(context.Context, []byte) error { return errors.New("unsupported") }, nil
			}
			return func(ctx context.Context, data []byte) error {
				publishAttempts = append(publishAttempts, string(data))
				if len(publishAttempts) == 1 {
					<-ctx.Done()
					return ctx.Err()
				}
				return nil
			}, nil
		}, cfg, tx, nil)

		worker.Start(ctx, 1)

		wantAttempts := []string{"first request", "second request", "first request"}
		if !reflect.DeepEqual(publishAttempts, wantAttempts) {
			t.Fatalf("publish attempts = %v, want %v", publishAttempts, wantAttempts)
		}
		wantProcessed := [][]string{{"43"}, {"42"}}
		wantRetried := [][]string{{"42", "44"}}
		if !reflect.DeepEqual(processed, wantProcessed) || !reflect.DeepEqual(retried, wantRetried) {
			t.Fatalf("processed = %v and retryable = %v, want %v and %v", processed, retried, wantProcessed, wantRetried)
		}
		if len(processed) == 2 {
			// Both messages succeeded, the test can stop the worker.
			cancel()
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
				var fetchCount, published int
				retryablePersisted := false
				queue := dispatchQueue{
					fetch: func() ([]entity.OutboxMessage, error) {
						fetchCount++
						if stage == "fetch" && fetchCount == 1 {
							return nil, storageErr
						}
						stored := int32(0)
						if retryablePersisted {
							// The retry attempt was persisted.
							stored = 1
						}
						return []entity.OutboxMessage{{IdempotencyKey: "42", Kind: accessRequestKind, Attempts: stored}}, nil
					},
					processed: func(keys []string) error {
						if !reflect.DeepEqual(keys, []string{"42"}) {
							t.Errorf("processed keys = %v, want [42]", keys)
						}
						if stage == "mark processed" && published == 1 {
							return storageErr
						}
						return nil
					},
					retryable: func(keys []string) error {
						if !reflect.DeepEqual(keys, []string{"42"}) {
							t.Errorf("retryable keys = %v, want [42]", keys)
						}
						retryablePersisted = true
						if stage == "mark retryable" && published == 1 {
							return storageErr
						}
						return nil
					},
					failed: func([]string) error { return nil },
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
				cfg := outboxTestConfig(time.Second, 5*time.Second, 3)
				worker := New(zap.NewNop(), queue, func(int32) (KindHandler, error) {
					return func(ctx context.Context, _ []byte) error {
						published++
						if stage == "mark retryable" && published == 1 {
							return errors.New("broker unavailable")
						}
						return nil
					}, nil
				}, cfg, tx, nil)

				worker.Start(ctx, 1)

				if len(results) != 2 || !errors.Is(results[0], storageErr) || results[1] != nil {
					t.Fatalf("dispatch transactions = %v, want storage failure followed by success", results)
				}
			})
		})
	}
}

func TestAccessRequestFailsAfterMaxAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		attempts := int32(0)
		message := entity.OutboxMessage{IdempotencyKey: "42", Kind: accessRequestKind, Data: []byte(`{"user_id":"7","proposal_id":42}`)}
		var retried, failed [][]string
		queue := dispatchQueue{
			fetch: func() ([]entity.OutboxMessage, error) {
				if attempts >= 2 {
					return nil, nil
				}
				return []entity.OutboxMessage{{IdempotencyKey: "42", Kind: accessRequestKind, Data: message.Data, Attempts: attempts}}, nil
			},
			processed: func([]string) error { return nil },
			retryable: func(keys []string) error {
				retried = append(retried, keys)
				attempts++
				return nil
			},
			failed: func(keys []string) error {
				failed = append(failed, keys)
				cancel()
				return nil
			},
		}
		failures := &recordingFailures{}
		cfg := outboxTestConfig(time.Second, time.Second, 2)
		tx := dispatchTx(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
		worker := New(zap.NewNop(), queue, func(int32) (KindHandler, error) {
			return func(context.Context, []byte) error { return errors.New("broker unavailable") }, nil
		}, cfg, tx, failures)

		worker.Start(ctx, 1)

		if want := [][]string{{"42"}}; !reflect.DeepEqual(retried, want) {
			t.Fatalf("retryable = %v, want %v", retried, want)
		}
		if want := [][]string{{"42"}}; !reflect.DeepEqual(failed, want) {
			t.Fatalf("failed = %v, want %v", failed, want)
		}
		wantNotifications := []string{"7|42|42"}
		if !reflect.DeepEqual(failures.calls, wantNotifications) {
			t.Fatalf("failure notifications = %v, want %v", failures.calls, wantNotifications)
		}
	})
}
