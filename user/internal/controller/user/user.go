package user

import (
	"context"

	userv1 "avito-ai-buddy/pkg/generated/user/v1"
	"avito-ai-buddy/user/internal/entity"

	"go.uber.org/zap"
)

type UserService interface {
	Create(ctx context.Context, profile entity.Profile, internship entity.Internship) (int64, error)
	GetMetadata(ctx context.Context, userID int64) (entity.Metadata, error)
}

type userServer struct {
	userv1.UnimplementedUserServiceServer
	service UserService
	logger  *zap.Logger
}

func NewUserServer(service UserService, logger *zap.Logger) *userServer {
	return &userServer{service: service, logger: logger}
}
