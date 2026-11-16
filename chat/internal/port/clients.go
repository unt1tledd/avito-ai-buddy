package port

import (
	"context"
	"fmt"

	"avito-ai-buddy/chat/internal/entity"
)

// CreateAccessRequest is the payload published into Kafka when an intern
// confirms an access proposal.
type CreateAccessRequest struct {
	UserID      string `json:"user_id"`
	ProposalID  int64  `json:"proposal_id"`
	ResourceID  string `json:"resource_id"`
	AccessLevel string `json:"access_level"`
	DispatchID  string `json:"dispatch_id,omitempty"`
}

// TaskEvent is a task status change consumed from Kafka.
type TaskEvent struct {
	ProposalID        int64             `json:"proposal_id,string,omitempty"`
	UserID            string            `json:"user_id"`
	TaskID            string            `json:"task_id"`
	ExternalRequestID string            `json:"external_request_id,omitempty"`
	Status            entity.TaskStatus `json:"status"`
	DispatchID        string            `json:"dispatch_id,omitempty"`
}

// IdempotencyKey scopes lifecycle notifications to a dispatch attempt.
// Decisions retain the existing external-ID / task-and-status keys.
func (e TaskEvent) IdempotencyKey() string {
	if e.Status == entity.TaskPending || e.Status == entity.TaskFailed {
		return "proposal:" + fmt.Sprint(e.ProposalID) + "|" + e.DispatchID + "|" + string(e.Status)
	}
	if e.ExternalRequestID != "" {
		return e.ExternalRequestID
	}
	return e.TaskID + "|" + string(e.Status)
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
