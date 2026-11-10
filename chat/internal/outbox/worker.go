package outbox

import (
	"context"
	"sync"
	"time"

	"avito-ai-buddy/chat/internal/config"
	"avito-ai-buddy/chat/internal/entity"

	"go.uber.org/zap"
)

type (
	outboxRepository interface {
		GetMessages(ctx context.Context, batchSize int32, inProgressTTL time.Duration) ([]entity.OutboxMessage, error)
		MarkAsProcessed(ctx context.Context, idempotencyKeys []string) error
		MarkAsRetryable(ctx context.Context, idempotencyKeys []string) error
	}

	transactor interface {
		WithTx(ctx context.Context, f func(ctx context.Context) error) (err error)
	}
)

type GlobalHandler = func(kind int32) (KindHandler, error)
type KindHandler = func(ctx context.Context, data []byte) error

type Outbox interface {
	Start(ctx context.Context, workers int, batchSize int32, waitTime time.Duration, inProgressTTL time.Duration)
}

type outboxImpl struct {
	logger           *zap.Logger
	outboxRepository outboxRepository
	globalHandler    GlobalHandler
	cfg              *config.Config
	transactor       transactor
}

func New(
	logger *zap.Logger,
	outboxRepository outboxRepository,
	globalHandler GlobalHandler,
	cfg *config.Config,
	transactor transactor,
) *outboxImpl {
	return &outboxImpl{
		logger:           logger,
		outboxRepository: outboxRepository,
		globalHandler:    globalHandler,
		cfg:              cfg,
		transactor:       transactor,
	}
}

func (o *outboxImpl) Start(
	ctx context.Context,
	workers int,
	batchSize int32,
	fetchPeriod time.Duration,
	inProgressTTL time.Duration,
) {
	wg := new(sync.WaitGroup)

	for workerID := 1; workerID <= workers; workerID++ {
		wg.Add(1)
		go o.worker(ctx, wg, batchSize, fetchPeriod, inProgressTTL)
	}
	wg.Wait()
}

func (o *outboxImpl) worker(
	ctx context.Context,
	wg *sync.WaitGroup,
	batchSize int32,
	waitTime time.Duration,
	inProgressTTL time.Duration,
) {
	defer wg.Done()
	timer := time.NewTimer(waitTime)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			delay := waitTime
			err := o.transactor.WithTx(ctx, func(ctx context.Context) error {
				messages, err := o.outboxRepository.GetMessages(ctx, batchSize, inProgressTTL)

				if err != nil {
					o.logger.Error("can not fetch messages from outbox", zap.Error(err))
					return err
				}

				successKeys := make([]string, 0, len(messages)/2)
				failedKeys := make([]string, 0, len(messages)/2)

				for i := range messages {
					message := messages[i]
					key := message.IdempotencyKey

					kindHandler, errKind := o.globalHandler(message.Kind)

					if errKind != nil {
						o.logger.Error("unexpected kind",
							zap.Error(errKind),
							zap.Any("kind", message.Kind),
							zap.String("key", key),
							zap.ByteString("data", message.Data),
						)
						continue
					}

					publishCtx, cancel := context.WithTimeout(ctx, o.cfg.Outbox.PublishTimeout)
					err = kindHandler(publishCtx, message.Data)
					cancel()

					if err != nil {
						delay = o.cfg.Outbox.RetryDelay
						failedKeys = append(failedKeys, key)
						o.logger.Error("kind error", zap.Error(err))
						continue
					}

					successKeys = append(successKeys, key)
				}

				err = o.outboxRepository.MarkAsProcessed(ctx, successKeys)

				if err != nil {
					o.logger.Error("mark as processed outbox error", zap.Error(err))
					return err
				}

				err = o.outboxRepository.MarkAsRetryable(ctx, failedKeys)

				if err != nil {
					o.logger.Error("mark as retryable error", zap.Error(err))
					return err
				}

				return nil
			})

			if err != nil {
				o.logger.Error("worker stage error", zap.Error(err))
			}
			timer.Reset(delay)
		}
	}
}
