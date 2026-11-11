package port

import (
	"context"

	"avito-ai-buddy/chat/internal/entity"
)

// UserContext is the intern profile snippet the AI service needs to answer.
type UserContext struct {
	UserID string
	TeamID string
	Role   string
}

// OnboardingStep is the onboarding step the intern is currently working on.
type OnboardingStep struct {
	ID         string
	Title      string
	ResourceID string
}

// AnswerRequest carries everything the AI service needs to produce an answer:
// the user profile, the question, the conversation history and the current
// onboarding step.
type AnswerRequest struct {
	User        UserContext
	Question    string
	History     []entity.Message
	CurrentStep *OnboardingStep
}

// Answer is the AI reply: the text, the cited sources and an optional access
// proposal the intern may confirm.
type Answer struct {
	Text     string
	Sources  []entity.Source
	Proposal *entity.Action
}

// CreateAccessRequest is the payload published into Kafka when an intern
// confirms an access proposal.
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

// IdempotencyKey returns the dedup key of the event: the external request ID
// when present, otherwise the task ID and status pair.
func (e TaskEvent) IdempotencyKey() string {
	if e.ExternalRequestID != "" {
		return e.ExternalRequestID
	}
	return e.TaskID + "|" + e.Status
}

// TaskEventsNotifier publishes received task status updates to chat subscribers.
type TaskEventsNotifier interface {
	PublishTaskStatus(ctx context.Context, event TaskEvent) error
}

// TokenValidator checks an access token with the auth service and returns
// the authenticated user ID.
type TokenValidator interface {
	ValidateToken(ctx context.Context, token string) (int64, error)
}

// Transactor runs a function inside a database transaction, rolling back
// when the function returns an error.
type Transactor interface {
	WithTx(ctx context.Context, fn func(context.Context) error) error
}
