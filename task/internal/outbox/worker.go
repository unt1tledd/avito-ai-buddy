package outbox

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"avito-ai-buddy/task/internal/config"
	"avito-ai-buddy/task/internal/port"
)

type transactor interface {
	WithTx(context.Context, func(context.Context) error) error
}

type publisher interface {
	PublishTaskStatus(context.Context, int64, []byte) error
}

type Worker struct {
	logger    *slog.Logger
	repo      port.OutboxQueue
	tx        transactor
	publisher publisher
	cfg       config.OutboxConfig
}

func New(logger *slog.Logger, repo port.OutboxQueue, tx transactor, publisher publisher, cfg config.OutboxConfig) *Worker {
	return &Worker{logger: logger, repo: repo, tx: tx, publisher: publisher, cfg: cfg}
}

func (w *Worker) Start(ctx context.Context) {
	var workers sync.WaitGroup
	for range w.cfg.Workers {
		workers.Go(func() { w.run(ctx) })
	}
	workers.Wait()
}

func (w *Worker) run(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			delay := w.cfg.PollInterval
			if err := w.dispatch(ctx); err != nil && ctx.Err() == nil {
				w.logger.Error("outbox dispatch failed", "error", err)
				delay = w.cfg.RetryDelay
			}
			timer.Reset(delay)
		}
	}
}

func (w *Worker) dispatch(ctx context.Context) error {
	return w.tx.WithTx(ctx, func(ctx context.Context) error {
		messages, err := w.repo.GetMessages(ctx, w.cfg.BatchSize)
		if err != nil {
			return err
		}

		var processed, retryable, failed []int64
		for _, message := range messages {
			if err := ctx.Err(); err != nil {
				return err
			}
			publishCtx, cancel := context.WithTimeout(ctx, w.cfg.PublishTimeout)
			err := w.publisher.PublishTaskStatus(publishCtx, message.TaskID, message.Payload)
			cancel()
			if err == nil {
				processed = append(processed, message.TaskID)
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}

			attempt := message.Attempts + 1
			w.logger.Warn("outbox publish failed", "task_id", message.TaskID, "attempt", attempt, "error", err)
			if message.Failed || attempt >= w.cfg.MaxAttempts {
				failed = append(failed, message.TaskID)
			} else {
				retryable = append(retryable, message.TaskID)
			}
		}

		if len(processed) > 0 {
			if err := w.repo.MarkAsProcessed(ctx, processed); err != nil {
				return err
			}
		}
		if len(failed) > 0 {
			if err := w.repo.MarkAsFailed(ctx, failed, w.cfg.FailedRetryDelay); err != nil {
				return err
			}
		}
		if len(retryable) > 0 {
			return w.repo.MarkAsRetryable(ctx, retryable, w.cfg.RetryDelay)
		}
		return nil
	})
}
