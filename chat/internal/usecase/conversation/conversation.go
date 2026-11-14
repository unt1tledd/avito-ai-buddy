package conversation

//go:generate mockgen -source=conversation.go -destination=mocks/conversation_mocks.go -package=mocks

import (
	"context"

	"avito-ai-buddy/chat/internal/entity"
)

type repository interface {
	GetOrCreate(ctx context.Context, userID int64) (*entity.Conversation, error)
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

// GetForTask resolves persisted proposal ownership without creating an empty chat.
func (s *Service) GetForTask(ctx context.Context, userID int64, taskID string) (*entity.Conversation, error) {
	if repo, ok := s.repo.(interface {
		GetForTask(context.Context, int64, string) (*entity.Conversation, error)
	}); ok {
		return repo.GetForTask(ctx, userID, taskID)
	}
	return s.repo.GetOrCreate(ctx, userID)
}
