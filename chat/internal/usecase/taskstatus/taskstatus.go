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

type actionRepository interface {
	ApplyTaskStatus(ctx context.Context, userID int64, event port.TaskEvent) (int64, error)
}

type messageRepository interface {
	CreateIdempotent(ctx context.Context, message entity.Message, idempotencyKey int64) (bool, error)
}

type hubNotifier interface {
	PublishMessage(ctx context.Context, userID string, message entity.Message) error
}

//go:generate mockgen -source=taskstatus.go -destination=mocks/taskstatus_mocks.go -package=mocks

type Service struct {
	logger   *zap.Logger
	actions  actionRepository
	messages messageRepository
	hub      hubNotifier
}

// New builds the service: it needs the action repository, the idempotent
// message repository and the hub used for the live WebSocket push.
func New(logger *zap.Logger, actions actionRepository, messages messageRepository, hub hubNotifier) *Service {
	return &Service{logger: logger, actions: actions, messages: messages, hub: hub}
}

// SaveTaskStatus stores the message in the caller's transaction. A nil message
// means the event is already in the chat history.
func (s *Service) SaveTaskStatus(ctx context.Context, event port.TaskEvent) (*entity.Message, error) {
	userID, err := strconv.ParseInt(event.UserID, 10, 64)
	if err != nil || userID <= 0 {
		return nil, fmt.Errorf("invalid user id %q", event.UserID)
	}

	valid := false
	switch event.Status {
	case entity.TaskApproved, entity.TaskRejected:
		valid = event.TaskID != "" && event.ProposalID >= 0
	case entity.TaskPending:
		valid = event.TaskID != "" && event.ProposalID > 0
	case entity.TaskFailed:
		valid = event.TaskID == "" && event.ProposalID > 0
	}
	if !valid {
		return nil, fmt.Errorf("invalid task status")
	}
	conversationID, err := s.actions.ApplyTaskStatus(ctx, userID, event)
	if err != nil {
		return nil, fmt.Errorf("apply task status: %w", err)
	}

	if conversationID == 0 {
		return nil, nil
	}
	message := entity.Message{
		ConversationID: conversationID,
		Role:           entity.MessageRoleAssistant,
		Text:           statusText(event),
		CreatedAt:      time.Now().UTC(),
	}
	created, err := s.messages.CreateIdempotent(ctx, message, numericKey(event.IdempotencyKey()))
	if err != nil {
		return nil, fmt.Errorf("save task status message: %w", err)
	}
	if !created {
		return nil, nil
	}
	return &message, nil
}

// PublishSavedTaskStatus pushes a committed message to live subscribers.
func (s *Service) PublishSavedTaskStatus(ctx context.Context, event port.TaskEvent, message entity.Message) {
	if err := s.hub.PublishMessage(ctx, event.UserID, message); err != nil {
		s.logger.Error("push task status message to websocket subscribers",
			zap.Error(err),
			zap.String("user_id", event.UserID),
			zap.String("task_id", event.TaskID),
		)
	}
}

func statusText(event port.TaskEvent) string {
	switch event.Status {
	case entity.TaskPending:
		return fmt.Sprintf("Заявка %s создана и ожидает решения.", event.TaskID)
	case entity.TaskFailed:
		return "Не удалось создать заявку на доступ. Можно повторить отправку в карточке заявки."
	}
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
