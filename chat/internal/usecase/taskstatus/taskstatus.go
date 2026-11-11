// Package taskstatus turns task status events consumed from Kafka into
// assistant messages in the intern's conversation. The message is the source
// of truth: the intern sees the status in the history even after going
// offline, while the WebSocket push remains best effort.
package taskstatus

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/port"

	"go.uber.org/zap"
)

type conversationService interface {
	GetOrCreate(ctx context.Context, userID int64) (*entity.Conversation, error)
}

type messageRepository interface {
	CreateIdempotent(ctx context.Context, message entity.Message, idempotencyKey int64) (bool, error)
}

type hubNotifier interface {
	PublishMessage(ctx context.Context, userID string, message entity.Message) error
}

//go:generate mockgen -source=taskstatus.go -destination=mocks/taskstatus_mocks.go -package=mocks

type Service struct {
	logger        *zap.Logger
	conversations conversationService
	messages      messageRepository
	hub           hubNotifier
}

// New builds the service: it needs the conversation service, the idempotent
// message repository and the hub used for the live WebSocket push.
func New(logger *zap.Logger, conversations conversationService, messages messageRepository, hub hubNotifier) *Service {
	return &Service{logger: logger, conversations: conversations, messages: messages, hub: hub}
}

// PublishTaskStatus implements port.TaskEventsNotifier. It makes Hub satisfy
// the same interface for live subscribers; this service adds persistence.
func (s *Service) PublishTaskStatus(ctx context.Context, event port.TaskEvent) error {
	userID, err := strconv.ParseInt(event.UserID, 10, 64)
	if err != nil || userID <= 0 {
		return fmt.Errorf("invalid user id %q", event.UserID)
	}

	conversation, err := s.conversations.GetOrCreate(ctx, userID)
	if err != nil {
		return fmt.Errorf("get conversation: %w", err)
	}

	message := entity.Message{
		ConversationID: conversation.ID,
		Role:           entity.MessageRoleAssistant,
		Text:           statusText(event),
		CreatedAt:      time.Now().UTC(),
	}
	created, err := s.messages.CreateIdempotent(ctx, message, numericKey(event.IdempotencyKey()))
	if err != nil {
		return fmt.Errorf("save task status message: %w", err)
	}
	if !created {
		// A redelivered event whose message is already in the history.
		return nil
	}

	// Fire and forget: the message is already persisted, so a failed push
	// only delays the live update, the history is always up to date.
	if err := s.hub.PublishMessage(ctx, event.UserID, message); err != nil {
		s.logger.Error("push task status message to websocket subscribers",
			zap.Error(err),
			zap.String("user_id", event.UserID),
			zap.String("task_id", event.TaskID),
		)
	}
	return nil
}

func statusText(event port.TaskEvent) string {
	return fmt.Sprintf("Статус задачи %s изменён: %s.", event.TaskID, event.Status)
}

// numericKey converts the event dedup key into the bigint idempotency key
// stored with the message. FNV-1a keeps collisions negligible.
func numericKey(key string) int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key))
	value := int64(hash.Sum64() & uint64(math.MaxInt64))
	if value == 0 {
		return 1
	}
	return value
}
