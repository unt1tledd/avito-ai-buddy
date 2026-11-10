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

func (s *actionService) ConfirmProposal(ctx context.Context, token, proposalID string) (entity.AccessRequest, error) {
	userID, err := s.auth.ValidateToken(ctx, token)
	if err != nil {
		return entity.AccessRequest{}, err
	}

	actionID, err := strconv.ParseInt(strings.TrimSpace(proposalID), 10, 64)
	if err != nil || userID <= 0 || actionID <= 0 {
		return entity.AccessRequest{}, xerrors.ErrMissingConfirmationIDs
	}
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
			IdempotencyKey: strconv.FormatInt(actionID, 10),
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
