package conversation

import (
	"context"
	"errors"
	"strconv"
	"time"

	"avito-ai-buddy/chat/internal/controller"
	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	chatpb "avito-ai-buddy/pkg/generated/chat/v1"
	"avito-ai-buddy/pkg/observability"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sessionService interface {
	GetSession(context.Context, string) (string, time.Time, error)
}

type service interface {
	List(context.Context, int64) ([]entity.ConversationSummary, error)
	Create(context.Context, int64) (entity.ConversationSummary, error)
	Delete(context.Context, int64, int64) (bool, error)
	DeleteAll(context.Context, int64) error
}

// Server implements ConversationService over the conversation use case.
type Server struct {
	conversations service
	sessions      sessionService
	logger        *zap.Logger
}

// NewServer builds the ConversationService gRPC server.
func NewServer(conversations service, sessions sessionService, logger *zap.Logger) *Server {
	return &Server{conversations: conversations, sessions: sessions, logger: logger}
}

func (s *Server) userID(ctx context.Context) (int64, error) {
	token, err := controller.GetAccessTokenFromContext(ctx)
	if err != nil {
		return 0, err
	}
	owner, _, err := s.sessions.GetSession(ctx, token)
	if errors.Is(err, chaterrors.ErrAuthUnavailable) {
		return 0, status.Error(codes.Unavailable, "session validation unavailable")
	}
	if err != nil {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	id, err := strconv.ParseInt(owner, 10, 64)
	if err != nil || id <= 0 {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	return id, nil
}

// ListConversations returns the authenticated intern's visible conversations.
func (s *Server) ListConversations(ctx context.Context, req *chatpb.ListConversationsRequest) (*chatpb.ListConversationsResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("conversation request validation failed", zap.String("rpc", "ListConversations"), zap.String("request_id", observability.RequestID(ctx)))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.conversations.List(ctx, userID)
	if err != nil {
		s.logger.Error("list conversations", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to list conversations")
	}
	response := &chatpb.ListConversationsResponse{Conversations: make([]*chatpb.Conversation, 0, len(items))}
	for _, item := range items {
		response.Conversations = append(response.Conversations, toProto(item))
	}
	if err := response.Validate(); err != nil {
		s.logger.Error("invalid conversation list", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to list conversations")
	}
	return response, nil
}

// CreateConversation creates a conversation for the authenticated intern.
func (s *Server) CreateConversation(ctx context.Context, req *chatpb.CreateConversationRequest) (*chatpb.Conversation, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("conversation request validation failed", zap.String("rpc", "CreateConversation"), zap.String("request_id", observability.RequestID(ctx)))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	item, err := s.conversations.Create(ctx, userID)
	if err != nil {
		s.logger.Error("create conversation", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to create conversation")
	}
	response := toProto(item)
	if err := response.Validate(); err != nil {
		s.logger.Error("invalid created conversation", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to create conversation")
	}
	return response, nil
}

// DeleteConversation soft-deletes a single conversation owned by the authenticated intern.
func (s *Server) DeleteConversation(ctx context.Context, req *chatpb.DeleteConversationRequest) (*chatpb.DeleteConversationResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("conversation request validation failed", zap.String("rpc", "DeleteConversation"), zap.String("request_id", observability.RequestID(ctx)))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	deleted, err := s.conversations.Delete(ctx, userID, req.GetId())
	if err != nil {
		s.logger.Error("delete conversation", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to delete conversation")
	}
	if !deleted {
		return nil, status.Error(codes.NotFound, "conversation not found")
	}
	return &chatpb.DeleteConversationResponse{}, nil
}

// DeleteAllConversations soft-deletes all conversations owned by the authenticated intern.
func (s *Server) DeleteAllConversations(ctx context.Context, req *chatpb.DeleteAllConversationsRequest) (*chatpb.DeleteAllConversationsResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("conversation request validation failed", zap.String("rpc", "DeleteAllConversations"), zap.String("request_id", observability.RequestID(ctx)))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.conversations.DeleteAll(ctx, userID); err != nil {
		s.logger.Error("delete all conversations", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to delete conversations")
	}
	return &chatpb.DeleteAllConversationsResponse{}, nil
}

func toProto(item entity.ConversationSummary) *chatpb.Conversation {
	return &chatpb.Conversation{Id: item.ID, Title: item.Title, CreatedAt: timestamppb.New(item.CreatedAt)}
}
