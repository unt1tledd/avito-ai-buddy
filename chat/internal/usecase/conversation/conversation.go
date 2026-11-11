package conversation

//go:generate mockgen -source=conversation.go -destination=mocks/conversation_mocks.go -package=mocks

import (
	"context"

	"avito-ai-buddy/chat/internal/entity"
)

type repository interface {
	GetOrCreate(ctx context.Context, userID int64) (*entity.Conversation, error)
}

type Service struct {
	repo repository
}

func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetOrCreate(ctx context.Context, userID int64) (*entity.Conversation, error) {
	return s.repo.GetOrCreate(ctx, userID)
}
