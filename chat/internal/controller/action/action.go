package action

//go:generate mockgen -source=action.go -destination=mocks/action_mocks.go -package=mocks

import (
	"context"
	"errors"

	"avito-ai-buddy/chat/internal/controller"
	"avito-ai-buddy/chat/internal/controller/converter"
	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	chatpb "avito-ai-buddy/pkg/generated/chat/v1"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type actionService interface {
	ConfirmProposal(ctx context.Context, token, proposalID string) (entity.AccessRequest, error)
}

type actionServer struct {
	actions actionService
	logger  *zap.Logger
}

func NewServer(actions actionService, logger *zap.Logger) *actionServer {
	return &actionServer{actions: actions, logger: logger}
}

func (s *actionServer) ConfirmProposal(ctx context.Context, req *chatpb.ConfirmProposalRequest) (*chatpb.ConfirmProposalResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("chat request validation failed", zap.String("rpc", "ConfirmProposal"), zap.Error(err))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	token, err := controller.GetAccessTokenFromContext(ctx)
	if err != nil {
		return nil, err
	}

	request, err := s.actions.ConfirmProposal(ctx, token, req.GetProposalId())
	if err != nil {
		switch {
		case errors.Is(err, chaterrors.ErrMissingConfirmationIDs):
			return nil, status.Error(codes.InvalidArgument, chaterrors.ErrMissingConfirmationIDs.Error())
		case errors.Is(err, chaterrors.ErrProposalNotFound):
			return nil, status.Error(codes.NotFound, chaterrors.ErrProposalNotFound.Error())
		case errors.Is(err, errors.ErrUnsupported):
			return nil, status.Error(codes.Unimplemented, "confirm proposal is not implemented")
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil, status.FromContextError(err).Err()
		case errors.Is(err, chaterrors.ErrInvalidToken):
			return nil, status.Error(codes.Unauthenticated, chaterrors.ErrInvalidToken.Error())
		case errors.Is(err, chaterrors.ErrAuthUnavailable):
			return nil, status.Error(codes.Unavailable, chaterrors.ErrAuthUnavailable.Error())
		default:
			s.logger.Error("confirm proposal failed", zap.Error(err))
			return nil, status.Error(codes.Internal, "confirm proposal failed")
		}
	}
	return &chatpb.ConfirmProposalResponse{AccessRequest: converter.AccessRequestToProto(request)}, nil
}
