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

	"avito-ai-buddy/pkg/observability"
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

// NewServer builds the ChatService gRPC server over the message usecase.
func NewServer(messages messageService, logger *zap.Logger) *chatServer {
	return &chatServer{messages: messages, logger: logger}
}

// SendMessage stores the intern's question and returns the assistant reply.
// Retries with the same idempotency key return the saved reply.
func (s *chatServer) SendMessage(ctx context.Context, req *chatpb.SendMessageRequest) (*chatpb.SendMessageResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("chat request validation failed", zap.String("rpc", "SendMessage"), zap.String("request_id", observability.RequestID(ctx)))
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
			s.logger.Error("auth dependency unavailable", zap.String("rpc", "SendMessage"), zap.String("request_id", observability.RequestID(ctx)), zap.Error(err))
			return nil, status.Error(codes.Unavailable, chaterrors.ErrAuthUnavailable.Error())
		case errors.Is(err, chaterrors.ErrInvalidMessageLength):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		case errors.Is(err, chaterrors.ErrIdempotencyKeyReused):
			return nil, status.Error(codes.AlreadyExists, chaterrors.ErrIdempotencyKeyReused.Error())
		case errors.Is(err, chaterrors.ErrReplyInProgress):
			return nil, status.Error(codes.Aborted, chaterrors.ErrReplyInProgress.Error())
		default:
			s.logger.Error("send message failed", zap.String("rpc", "SendMessage"), zap.String("request_id", observability.RequestID(ctx)), zap.String("stage", sendFailureStage(err)), zap.Error(err))
			return nil, status.Error(codes.Internal, "send message failed")
		}
	}
	return &chatpb.SendMessageResponse{
		AssistantMessage: converter.MessageToProto(assistantMessage),
	}, nil
}

// GetHistory returns a page of the conversation history ordered from newest
// to oldest, with a cursor for the next page.
func (s *chatServer) GetHistory(ctx context.Context, req *chatpb.GetHistoryRequest) (*chatpb.GetHistoryResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("chat request validation failed", zap.String("rpc", "GetHistory"), zap.String("request_id", observability.RequestID(ctx)))
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
			s.logger.Error("auth dependency unavailable", zap.String("rpc", "GetHistory"), zap.String("request_id", observability.RequestID(ctx)), zap.Error(err))
			return nil, status.Error(codes.Unavailable, chaterrors.ErrAuthUnavailable.Error())
		default:
			s.logger.Error("get history failed", zap.String("rpc", "GetHistory"), zap.String("request_id", observability.RequestID(ctx)), zap.Error(err))
			return nil, status.Error(codes.Internal, "get history failed")
		}
	}
	return converter.HistoryToProto(page.Messages, page.NextPageToken), nil
}

func sendFailureStage(err error) string {
	switch {
	case errors.Is(err, chaterrors.ErrGetConversation):
		return "get_conversation"
	case errors.Is(err, chaterrors.ErrSaveUserMessage):
		return "save_question"
	case errors.Is(err, chaterrors.ErrListMessages):
		return "load_history"
	case errors.Is(err, chaterrors.ErrGetUserContext):
		return "get_user_context"
	case errors.Is(err, chaterrors.ErrGetOnboardingPlans):
		return "get_onboarding_plans"
	case errors.Is(err, chaterrors.ErrGetAIAnswer):
		return "get_ai_answer"
	case errors.Is(err, chaterrors.ErrSaveAssistantMessage):
		return "save_answer"
	case errors.Is(err, chaterrors.ErrSaveProposal):
		return "save_proposal"
	default:
		return "process_message"
	}
}
