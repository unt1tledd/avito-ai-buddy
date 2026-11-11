package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	"avito-ai-buddy/task/internal/entity"
	taskerrors "avito-ai-buddy/task/internal/errors"
)

type creator interface {
	Create(context.Context, entity.CreateRequest) (entity.AccessRequest, error)
}

type CreateHandler struct {
	tasks creator
}

func NewCreateHandler(tasks creator) *CreateHandler {
	return &CreateHandler{tasks: tasks}
}

func (h *CreateHandler) Handle(ctx context.Context, payload []byte) error {
	var request struct {
		UserID      int64  `json:"user_id,string"`
		ProposalID  int64  `json:"proposal_id,string"`
		ResourceID  string `json:"resource_id"`
		AccessLevel string `json:"access_level"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		return fmt.Errorf("%w: decode create access request: %v", taskerrors.ErrInvalidInput, err)
	}
	_, err := h.tasks.Create(ctx, entity.CreateRequest{
		ProposalID: request.ProposalID, UserID: request.UserID,
		ResourceID: request.ResourceID, AccessLevel: request.AccessLevel,
	})
	return err
}
