package chat

//go:generate mockgen -source=chat.go -destination=mocks/chat_mocks.go -package=mocks

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

type messageService interface {
	SendMessage(ctx context.Context, token, text string, idempotencyKey int64) (entity.Message, error)
	GetHistory(ctx context.Context, token string, pageSize int32, pageToken string) (entity.HistoryPage, error)
}

type chatServer struct {
	messages messageService
	logger   *zap.Logger
}

func NewServer(messages messageService, logger *zap.Logger) *chatServer {
	return &chatServer{messages: messages, logger: logger}
}

func (s *chatServer) SendMessage(ctx context.Context, req *chatpb.SendMessageRequest) (*chatpb.SendMessageResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("chat request validation failed", zap.String("rpc", "SendMessage"), zap.Error(err))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	token, err := controller.GetAccessTokenFromContext(ctx)
	if err != nil {
		return nil, err
	}

	assistantMessage, err := s.messages.SendMessage(ctx, token, req.GetText(), req.GetIdempotencyKey())
	if err != nil {
		switch {
		case errors.Is(err, errors.ErrUnsupported):
			return nil, status.Error(codes.Unimplemented, "send message is not implemented")
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil, status.FromContextError(err).Err()
		case errors.Is(err, chaterrors.ErrInvalidToken):
			return nil, status.Error(codes.Unauthenticated, chaterrors.ErrInvalidToken.Error())
		case errors.Is(err, chaterrors.ErrAuthUnavailable):
			return nil, status.Error(codes.Unavailable, chaterrors.ErrAuthUnavailable.Error())
		case errors.Is(err, chaterrors.ErrIdempotencyKeyReused):
			return nil, status.Error(codes.AlreadyExists, chaterrors.ErrIdempotencyKeyReused.Error())
		case errors.Is(err, chaterrors.ErrReplyInProgress):
			return nil, status.Error(codes.Aborted, chaterrors.ErrReplyInProgress.Error())
		default:
			s.logger.Error("send message failed", zap.Error(err))
			return nil, status.Error(codes.Internal, "send message failed")
		}
	}
	return &chatpb.SendMessageResponse{
		AssistantMessage: converter.MessageToProto(assistantMessage),
	}, nil
}

func (s *chatServer) GetHistory(ctx context.Context, req *chatpb.GetHistoryRequest) (*chatpb.GetHistoryResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("chat request validation failed", zap.String("rpc", "GetHistory"), zap.Error(err))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	token, err := controller.GetAccessTokenFromContext(ctx)
	if err != nil {
		return nil, err
	}

	page, err := s.messages.GetHistory(ctx, token, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		switch {
		case errors.Is(err, errors.ErrUnsupported):
			return nil, status.Error(codes.Unimplemented, "get history is not implemented")
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil, status.FromContextError(err).Err()
		case errors.Is(err, chaterrors.ErrInvalidToken):
			return nil, status.Error(codes.Unauthenticated, chaterrors.ErrInvalidToken.Error())
		case errors.Is(err, chaterrors.ErrAuthUnavailable):
			return nil, status.Error(codes.Unavailable, chaterrors.ErrAuthUnavailable.Error())
		default:
			s.logger.Error("get history failed", zap.Error(err))
			return nil, status.Error(codes.Internal, "get history failed")
		}
	}
	return converter.HistoryToProto(page.Messages, page.NextPageToken), nil
}
