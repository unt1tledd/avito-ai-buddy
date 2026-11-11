package controller

import (
	userv1 "avito-ai-buddy/pkg/generated/user/v1"
	"avito-ai-buddy/user/internal/controller/user"

	"go.uber.org/zap"
	"google.golang.org/grpc"
)

type API struct {
	userServer userv1.UserServiceServer
}

func New(service user.UserService, logger *zap.Logger) *API {
	return &API{userServer: user.NewUserServer(service, logger)}
}

func (a *API) Register(server *grpc.Server) {
	userv1.RegisterUserServiceServer(server, a.userServer)
}
