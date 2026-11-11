package port

import (
	"context"

	"avito-ai-buddy/chat/internal/entity"
)

type UserContext struct {
	UserID string
	TeamID string
	Role   string
}

type OnboardingStep struct {
	ID         string
	Title      string
	ResourceID string
}

type AnswerRequest struct {
	User        UserContext
	Question    string
	History     []entity.Message
	CurrentStep *OnboardingStep
}

type Answer struct {
	Text     string
	Sources  []entity.Source
	Proposal *entity.Action
}

type CreateAccessRequest struct {
	UserID      string `json:"user_id"`
	ProposalID  int64  `json:"proposal_id"`
	ResourceID  string `json:"resource_id"`
	AccessLevel string `json:"access_level"`
}

// TaskEvent is a task status change consumed from Kafka.
type TaskEvent struct {
	UserID            string `json:"user_id"`
	TaskID            string `json:"task_id"`
	ExternalRequestID string `json:"external_request_id,omitempty"`
	Status            string `json:"status"`
}

// TaskEventsNotifier publishes received task status updates to chat subscribers.
type TaskEventsNotifier interface {
	PublishTaskStatus(ctx context.Context, event TaskEvent) error
}

type TokenValidator interface {
	ValidateToken(ctx context.Context, token string) (int64, error)
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(context.Context) error) error
}
