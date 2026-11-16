package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"avito-ai-buddy/task/internal/entity"
	taskerrors "avito-ai-buddy/task/internal/errors"
)

type repository interface {
	GetByProposal(context.Context, int64) (entity.AccessRequest, error)
	Create(context.Context, entity.CreateRequest, int64) (entity.AccessRequest, error)
	ListPending(context.Context, int64, int64, int32) ([]entity.AccessRequest, error)
	GetUserTasks(context.Context, int64, int64, int32) ([]entity.AccessRequest, error)
	Decide(context.Context, entity.Decision) (entity.AccessRequest, error)
}

type userClient interface {
	GetTeamID(context.Context, int64) (int64, error)
}

type Service struct {
	repository repository
	users      userClient
}

func NewService(repo repository, users userClient) *Service {
	return &Service{repository: repo, users: users}
}

// GetByProposal reconciles a command after an ambiguous create/publish error.
func (s *Service) GetByProposal(ctx context.Context, proposalID int64) (entity.AccessRequest, error) {
	if proposalID <= 0 {
		return entity.AccessRequest{}, taskerrors.ErrInvalidInput
	}
	return s.repository.GetByProposal(ctx, proposalID)
}

func (s *Service) Create(ctx context.Context, request entity.CreateRequest) (entity.AccessRequest, error) {
	request.ResourceID = strings.TrimSpace(request.ResourceID)
	request.AccessLevel = strings.TrimSpace(request.AccessLevel)
	if request.ProposalID <= 0 || request.UserID <= 0 || request.ResourceID == "" || request.AccessLevel == "" ||
		strings.ContainsRune(request.ResourceID, 0) || strings.ContainsRune(request.AccessLevel, 0) {
		return entity.AccessRequest{}, taskerrors.ErrInvalidInput
	}

	existing, err := s.repository.GetByProposal(ctx, request.ProposalID)
	if errors.Is(err, taskerrors.ErrNotFound) {
		teamID, err := s.users.GetTeamID(ctx, request.UserID)
		if err != nil {
			return entity.AccessRequest{}, fmt.Errorf("load intern team: %w", err)
		}
		return s.repository.Create(ctx, request, teamID)
	}
	if err != nil {
		return entity.AccessRequest{}, err
	}
	if existing.UserID != request.UserID || existing.ResourceID != request.ResourceID || existing.AccessLevel != request.AccessLevel {
		return entity.AccessRequest{}, taskerrors.ErrProposalConflict
	}
	return existing, nil
}

func (s *Service) ListPending(ctx context.Context, teamID, afterID int64, limit int32) ([]entity.AccessRequest, error) {
	if teamID <= 0 || afterID < 0 || limit < 1 || limit > 100 {
		return nil, taskerrors.ErrInvalidInput
	}
	return s.repository.ListPending(ctx, teamID, afterID, limit)
}

func (s *Service) GetUserTasks(ctx context.Context, userID, afterID int64, limit int32) ([]entity.AccessRequest, error) {
	if userID <= 0 || afterID < 0 || limit < 1 || limit > 100 {
		return nil, taskerrors.ErrInvalidInput
	}
	return s.repository.GetUserTasks(ctx, userID, afterID, limit)
}

func (s *Service) Decide(ctx context.Context, decision entity.Decision) (entity.AccessRequest, error) {
	if decision.TaskID <= 0 || decision.TeamID <= 0 || decision.DecidedByUserID <= 0 ||
		(decision.Status != entity.Approved && decision.Status != entity.Rejected) ||
		strings.ContainsRune(decision.Comment, 0) || strings.ContainsRune(decision.ExternalRequestID, 0) {
		return entity.AccessRequest{}, taskerrors.ErrInvalidInput
	}
	decision.Comment = strings.TrimSpace(decision.Comment)
	decision.ExternalRequestID = strings.TrimSpace(decision.ExternalRequestID)
	return s.repository.Decide(ctx, decision)
}
