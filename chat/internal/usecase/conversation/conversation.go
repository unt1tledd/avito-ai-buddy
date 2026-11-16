package conversation

//go:generate mockgen -source=conversation.go -destination=mocks/conversation_mocks.go -package=mocks

import (
	"context"

	"avito-ai-buddy/chat/internal/entity"
)

type repository interface {
	GetOrCreate(ctx context.Context, userID int64) (*entity.Conversation, error)
	List(context.Context, int64) ([]entity.ConversationSummary, error)
	Create(context.Context, int64) (entity.ConversationSummary, error)
	Delete(context.Context, int64, int64) (bool, error)
	DeleteAll(context.Context, int64) error
}

// Service resolves an intern to their one-on-one conversation, creating it
// on first use.
type Service struct {
	repo repository
}

// NewService builds the conversation service over its repository.
func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

// GetOrCreate returns the intern's conversation, creating it on first use.
func (s *Service) GetOrCreate(ctx context.Context, userID int64) (*entity.Conversation, error) {
	return s.repo.GetOrCreate(ctx, userID)
}

func (s *Service) List(ctx context.Context, userID int64) ([]entity.ConversationSummary, error) {
	return s.repo.List(ctx, userID)
}

func (s *Service) Create(ctx context.Context, userID int64) (entity.ConversationSummary, error) {
	return s.repo.Create(ctx, userID)
}

func (s *Service) Delete(ctx context.Context, userID, conversationID int64) (bool, error) {
	return s.repo.Delete(ctx, userID, conversationID)
}

func (s *Service) DeleteAll(ctx context.Context, userID int64) error {
	return s.repo.DeleteAll(ctx, userID)
}
