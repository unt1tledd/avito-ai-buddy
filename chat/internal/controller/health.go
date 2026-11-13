package controller

import (
	"context"

	"avito-ai-buddy/pkg/health"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

func RegisterHealth(ctx context.Context, mux *runtime.ServeMux, conn grpc.ClientConnInterface, pingDB, checkKafka func(context.Context) error) error {
	return health.RegisterGateway(ctx, mux, conn, pingDB, checkKafka)
}
