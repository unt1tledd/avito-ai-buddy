package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"avito-ai-buddy/task/internal/entity"
	taskerrors "avito-ai-buddy/task/internal/errors"
)

type creator interface {
	Create(context.Context, entity.CreateRequest) (entity.AccessRequest, error)
	GetByProposal(context.Context, int64) (entity.AccessRequest, error)
}

type statusPublisher interface {
	PublishTaskStatus(context.Context, int64, []byte) error
}

type createCommand struct {
	UserID      int64  `json:"user_id,string"`
	ProposalID  int64  `json:"proposal_id,string"`
	ResourceID  string `json:"resource_id"`
	AccessLevel string `json:"access_level"`
	DispatchID  string `json:"dispatch_id"`
}

type CreateHandler struct {
	tasks   creator
	results statusPublisher
}

func NewCreateHandler(tasks creator, results statusPublisher) *CreateHandler {
	return &CreateHandler{tasks: tasks, results: results}
}

func (h *CreateHandler) Handle(ctx context.Context, payload []byte) error {
	var request createCommand
	if err := json.Unmarshal(payload, &request); err != nil {
		return fmt.Errorf("%w: decode create access request: %v", taskerrors.ErrInvalidInput, err)
	}
	task, err := h.tasks.Create(ctx, entity.CreateRequest{
		ProposalID: request.ProposalID, UserID: request.UserID,
		ResourceID: request.ResourceID, AccessLevel: request.AccessLevel,
	})
	if err != nil {
		return err
	}
	// The command offset is committed only after Kafka acknowledges this result.
	// Redelivery safely repeats Create and the result, without creating a new task.
	return h.publish(ctx, task, request.DispatchID)
}

// ReportFailure runs after the DLQ write and before committing the command.
// A lost acknowledgement must not turn a successfully created task into failed.
func (h *CreateHandler) ReportFailure(ctx context.Context, payload []byte) error {
	var request createCommand
	if json.Unmarshal(payload, &request) != nil || request.UserID <= 0 || request.ProposalID <= 0 {
		// An unidentifiable command remains in the DLQ; no user state can be changed.
		return nil
	}
	task, err := h.tasks.GetByProposal(ctx, request.ProposalID)
	if err == nil {
		if task.UserID != request.UserID {
			return nil
		}
		return h.publish(ctx, task, request.DispatchID)
	}
	if !errors.Is(err, taskerrors.ErrNotFound) {
		return err // Reconcile again before declaring that no task exists.
	}
	return h.publish(ctx, entity.AccessRequest{
		UserID: request.UserID, ProposalID: request.ProposalID, Status: entity.Failed,
	}, request.DispatchID)
}

func (h *CreateHandler) publish(ctx context.Context, task entity.AccessRequest, dispatchID string) error {
	payload, err := json.Marshal(entity.StatusEvent{
		UserID: task.UserID, ProposalID: task.ProposalID, TaskID: task.ID,
		Status: task.Status, ExternalRequestID: task.ExternalRequestID, DispatchID: dispatchID,
	})
	if err != nil {
		return fmt.Errorf("encode command result: %w", err)
	}
	key := task.ID
	if key == 0 {
		key = task.ProposalID
	}
	return h.results.PublishTaskStatus(ctx, key, payload)
}
