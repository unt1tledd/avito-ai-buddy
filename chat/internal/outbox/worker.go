package outbox

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"avito-ai-buddy/chat/internal/config"
	"avito-ai-buddy/chat/internal/entity"
	outboxfetch "avito-ai-buddy/chat/internal/repository/outbox"

	"go.uber.org/zap"
)

type (
	outboxRepository interface {
		GetMessages(ctx context.Context) ([]entity.OutboxMessage, error)
		MarkAsProcessed(ctx context.Context, idempotencyKeys []string) error
		MarkAsRetryable(ctx context.Context, idempotencyKeys []string) error
		MarkAsFailed(ctx context.Context, idempotencyKeys []string) error
	}

	transactor interface {
		WithTx(ctx context.Context, f func(ctx context.Context) error) (err error)
	}

	// FailureNotifier informs the user when an outbox message exhausts all
	// retry attempts and is parked as FAILED.
	FailureNotifier interface {
		PublishAccessRequestFailed(ctx context.Context, userID string, proposalID int64, idempotencyKey string) error
	}
)

// GlobalHandler resolves the delivery handler for a message kind, returning
// an error for kinds the service does not dispatch.
type GlobalHandler = func(kind int32) (KindHandler, error)

// KindHandler delivers a single outbox message payload.
type KindHandler = func(ctx context.Context, data []byte) error

// Outbox runs the dispatch workers until the context is cancelled.
type Outbox interface {
	Start(ctx context.Context, workers int)
}

type outboxImpl struct {
	logger           *zap.Logger
	outboxRepository outboxRepository
	globalHandler    GlobalHandler
	cfg              *config.Config
	transactor       transactor
	failureNotifier  FailureNotifier
}

// New builds the dispatcher: it needs the outbox repository, the kind
// handler resolver, the config, a transaction manager and the notifier used
// to inform users about messages parked as FAILED.
func New(
	logger *zap.Logger,
	outboxRepository outboxRepository,
	globalHandler GlobalHandler,
	cfg *config.Config,
	transactor transactor,
	failureNotifier FailureNotifier,
) *outboxImpl {
	return &outboxImpl{
		logger:           logger,
		outboxRepository: outboxRepository,
		globalHandler:    globalHandler,
		cfg:              cfg,
		transactor:       transactor,
		failureNotifier:  failureNotifier,
	}
}

// Start launches the dispatch workers and blocks until they finish.
func (o *outboxImpl) Start(ctx context.Context, workers int) {
	wg := new(sync.WaitGroup)

	for workerID := 1; workerID <= workers; workerID++ {
		wg.Add(1)
		go o.worker(ctx, wg)
	}
	wg.Wait()
}

func (o *outboxImpl) worker(
	ctx context.Context,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	timer := time.NewTimer(o.cfg.Outbox.PollInterval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			delay := o.cfg.Outbox.PollInterval
			err := o.transactor.WithTx(ctx, func(ctx context.Context) error {
				fetchCtx := outboxfetch.WithFetchOptions(ctx, outboxfetch.FetchOptions{
					BatchSize:     o.cfg.Outbox.BatchSize,
					InProgressTTL: o.cfg.Outbox.InProgressTTL,
				})
				messages, err := o.outboxRepository.GetMessages(fetchCtx)

				if err != nil {
					o.logger.Error("can not fetch messages from outbox", zap.Error(err))
					return err
				}

				successKeys := make([]string, 0, len(messages)/2)
				retryableKeys := make([]string, 0, len(messages)/2)
				var terminal []entity.OutboxMessage

				for i := range messages {
					message := messages[i]
					key := message.IdempotencyKey

					publishErr := o.publish(ctx, message)

					if publishErr == nil {
						successKeys = append(successKeys, key)
						continue
					}

					delay = o.cfg.Outbox.RetryDelay
					if message.Attempts+1 >= o.cfg.Outbox.MaxAttempts {
						// The last retry attempt failed: park the message as
						// FAILED and let the user know the request never left.
						terminal = append(terminal, message)
					} else {
						retryableKeys = append(retryableKeys, key)
					}
					o.logger.Error("outbox publish failed",
						zap.Error(publishErr),
						zap.String("key", key),
						zap.Int32("attempt", message.Attempts+1),
					)
				}

				if len(successKeys) > 0 {
					if err := o.outboxRepository.MarkAsProcessed(ctx, successKeys); err != nil {
						return err
					}
				}
				if len(terminal) > 0 {
					keys := messageKeys(terminal)
					if err := o.outboxRepository.MarkAsFailed(ctx, keys); err != nil {
						return err
					}
					o.notifyFailures(ctx, terminal)
				}
				if len(retryableKeys) > 0 {
					return o.outboxRepository.MarkAsRetryable(ctx, retryableKeys)
				}
				return nil
			})
			if err != nil {
				delay = o.cfg.Outbox.RetryDelay
				o.logger.Error("outbox dispatch failed", zap.Error(err))
			}

			timer.Reset(delay)
		}
	}
}

// publish decodes and delivers a single outbox message. An unsupported kind
// counts as a delivery failure so the message eventually reaches FAILED.
func (o *outboxImpl) publish(ctx context.Context, message entity.OutboxMessage) error {
	kindHandler, errKind := o.globalHandler(message.Kind)
	if errKind != nil {
		return errKind
	}

	publishCtx, cancel := context.WithTimeout(ctx, o.cfg.Outbox.PublishTimeout)
	defer cancel()
	return kindHandler(publishCtx, message.Data)
}

// notifyFailures tells the affected users that their access request could not
// be delivered. Notification failures are only logged: the outbox state is
// already terminal at this point.
func (o *outboxImpl) notifyFailures(ctx context.Context, terminal []entity.OutboxMessage) {
	if o.failureNotifier == nil {
		return
	}
	for _, message := range terminal {
		var request struct {
			UserID     string `json:"user_id"`
			ProposalID int64  `json:"proposal_id"`
		}
		if err := json.Unmarshal(message.Data, &request); err != nil {
			o.logger.Error("decode failed outbox message for notification",
				zap.Error(err),
				zap.String("key", message.IdempotencyKey),
			)
			continue
		}
		if err := o.failureNotifier.PublishAccessRequestFailed(ctx, request.UserID, request.ProposalID, message.IdempotencyKey); err != nil {
			o.logger.Error("publish access request failure notification",
				zap.Error(err),
				zap.String("key", message.IdempotencyKey),
			)
		}
	}
}

func messageKeys(messages []entity.OutboxMessage) []string {
	keys := make([]string, 0, len(messages))
	for _, message := range messages {
		keys = append(keys, message.IdempotencyKey)
	}
	return keys
}
