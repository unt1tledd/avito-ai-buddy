package action

//go:generate mockgen -source=action.go -destination=mocks/action_mocks.go -package=mocks

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"avito-ai-buddy/chat/internal/entity"
	xerrors "avito-ai-buddy/chat/internal/errors"
	"avito-ai-buddy/chat/internal/port"
)

type repository interface {
	GetByID(ctx context.Context, userID, proposalID int64) (*entity.Action, error)
	SaveAccessRequest(ctx context.Context, proposalID int64, request entity.AccessRequest) error
}

type outboxRepository interface {
	SendOutboxMessage(ctx context.Context, message entity.OutboxMessage) error
	GetMessageStatus(ctx context.Context, idempotencyKey string) (string, int32, error)
	ResetMessage(ctx context.Context, idempotencyKey string) error
}

type actionService struct {
	auth    port.TokenValidator
	actions repository
	outbox  outboxRepository
	tx      port.Transactor
}

func NewService(actions repository, outbox outboxRepository, auth port.TokenValidator, tx port.Transactor) *actionService {
	return &actionService{actions: actions, outbox: outbox, auth: auth, tx: tx}
}

// ConfirmProposal enqueues an access request for the proposal. The request is
// created once (idempotent by proposal); re-confirming a FAILED dispatch
// resets the outbox message so the worker tries again.
func (s *actionService) ConfirmProposal(ctx context.Context, token, proposalID string) (entity.AccessRequest, error) {
	userID, err := s.auth.ValidateToken(ctx, token)
	if err != nil {
		return entity.AccessRequest{}, err
	}

	actionID, err := strconv.ParseInt(strings.TrimSpace(proposalID), 10, 64)
	if err != nil || userID <= 0 || actionID <= 0 {
		return entity.AccessRequest{}, xerrors.ErrMissingConfirmationIDs
	}
	idempotencyKey := strconv.FormatInt(actionID, 10)

	var request entity.AccessRequest
	err = s.tx.WithTx(ctx, func(txCtx context.Context) error {
		proposal, err := s.actions.GetByID(txCtx, userID, actionID)
		if err != nil {
			return fmt.Errorf("load proposal: %w", err)
		}
		if proposal == nil {
			return xerrors.ErrProposalNotFound
		}

		if proposal.AccessRequest != nil {
			request = *proposal.AccessRequest
			status, _, err := s.outbox.GetMessageStatus(txCtx, idempotencyKey)
			if err != nil {
				return fmt.Errorf("load outbox message: %w", err)
			}
			if status != entity.OutboxStatusFailed {
				// Already dispatched successfully or still in progress:
				// return the saved request without dispatching again.
				return nil
			}
			// The dispatch failed after all retries: re-confirmation resets
			// the outbox message so the worker tries again.
			request.Status = "pending_dispatch"
			if err := s.actions.SaveAccessRequest(txCtx, actionID, request); err != nil {
				return fmt.Errorf("save request status: %w", err)
			}
			if err := s.outbox.ResetMessage(txCtx, idempotencyKey); err != nil {
				return fmt.Errorf("re-enqueue access request: %w", err)
			}
			return nil
		}

		request.Status = "pending_dispatch"
		if err := s.actions.SaveAccessRequest(txCtx, actionID, request); err != nil {
			return fmt.Errorf("save request status: %w", err)
		}
		data, err := json.Marshal(port.CreateAccessRequest{
			UserID:      strconv.FormatInt(userID, 10),
			ProposalID:  actionID,
			ResourceID:  proposal.ResourceID,
			AccessLevel: proposal.AccessLevel,
		})
		if err != nil {
			return fmt.Errorf("encode access request: %w", err)
		}
		if err := s.outbox.SendOutboxMessage(txCtx, entity.OutboxMessage{
			IdempotencyKey: idempotencyKey,
			Kind:           entity.OutboxKindCreateAccessRequest,
			Data:           data,
		}); err != nil {
			return fmt.Errorf("enqueue access request: %w", err)
		}
		return nil
	})
	if err != nil {
		return entity.AccessRequest{}, err
	}
	return request, nil
}
