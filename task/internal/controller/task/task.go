package task

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskv1 "avito-ai-buddy/pkg/generated/task/v1"
	"avito-ai-buddy/task/internal/entity"
	taskerrors "avito-ai-buddy/task/internal/errors"
)

type service interface {
	ListPending(context.Context, int64, int64, int32) ([]entity.AccessRequest, error)
	GetUserTasks(context.Context, int64, int64, int32) ([]entity.AccessRequest, error)
	Decide(context.Context, entity.Decision) (entity.AccessRequest, error)
}

type tokenValidator interface {
	ValidateToken(context.Context, string) (int64, error)
}

type Controller struct {
	taskv1.UnimplementedTaskServiceServer

	tasks           service
	logger          *slog.Logger
	trackerToken    string
	auth            tokenValidator
	defaultPageSize int32
}

func NewController(tasks service, logger *slog.Logger, trackerToken string, auth tokenValidator, defaultPageSize int32) *Controller {
	return &Controller{tasks: tasks, logger: logger, trackerToken: trackerToken, auth: auth, defaultPageSize: defaultPageSize}
}

func (c *Controller) ListPending(ctx context.Context, req *taskv1.ListPendingRequest) (*taskv1.ListPendingResponse, error) {
	if err := c.authorize(ctx); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		c.logger.Warn("task request validation failed", "rpc", "ListPending", "error", err)
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	limit := c.defaultPageSize
	if req.Limit != nil {
		limit = req.GetLimit()
	}
	requests, err := c.tasks.ListPending(ctx, req.GetTeamId(), req.GetAfterId(), limit)
	if err != nil {
		return nil, c.rpcError(err)
	}
	response := &taskv1.ListPendingResponse{Tasks: make([]*taskv1.AccessRequest, 0, len(requests))}
	for _, request := range requests {
		response.Tasks = append(response.Tasks, toProto(request))
	}
	return response, nil
}

func (c *Controller) GetUserTasks(ctx context.Context, req *taskv1.GetUserTasksRequest) (*taskv1.GetUserTasksResponse, error) {
	if err := req.Validate(); err != nil {
		c.logger.Warn("task request validation failed", "rpc", "GetUserTasks", "error", err)
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) != 1 {
		return nil, status.Error(codes.Unauthenticated, "user access token required")
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" ||
		subtle.ConstantTimeCompare([]byte(token), []byte(c.trackerToken)) == 1 {
		return nil, status.Error(codes.Unauthenticated, "user access token required")
	}
	userID, err := c.auth.ValidateToken(ctx, token)
	if err != nil {
		return nil, c.rpcError(err)
	}
	if userID != req.GetUserId() {
		return nil, status.Error(codes.PermissionDenied, "task list belongs to another user")
	}
	limit := c.defaultPageSize
	if req.Limit != nil {
		limit = req.GetLimit()
	}
	requests, err := c.tasks.GetUserTasks(ctx, userID, req.GetAfterId(), limit)
	if err != nil {
		return nil, c.rpcError(err)
	}
	response := &taskv1.GetUserTasksResponse{Tasks: make([]*taskv1.AccessRequest, 0, len(requests))}
	for _, request := range requests {
		response.Tasks = append(response.Tasks, toProto(request))
	}
	return response, nil
}

func (c *Controller) Decide(ctx context.Context, req *taskv1.DecideRequest) (*taskv1.AccessRequest, error) {
	if err := c.authorize(ctx); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		c.logger.Warn("task request validation failed", "rpc", "Decide", "error", err)
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	decision := req.GetDecision()
	request, err := c.tasks.Decide(ctx, entity.Decision{
		TaskID: req.GetTaskId(), TeamID: decision.GetTeamId(), DecidedByUserID: decision.GetDecidedByUserId(),
		Status: decisionStatus(decision.GetStatus()), Comment: decision.GetComment(),
		ExternalRequestID: decision.GetExternalRequestId(),
	})
	if err != nil {
		return nil, c.rpcError(err)
	}
	return toProto(request), nil
}

func (c *Controller) authorize(ctx context.Context) error {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) == 1 && c.trackerToken != "" {
		token, ok := strings.CutPrefix(values[0], "Bearer ")
		if ok && subtle.ConstantTimeCompare([]byte(token), []byte(c.trackerToken)) == 1 {
			return nil
		}
	}
	return status.Error(codes.Unauthenticated, "valid tracker credentials required")
}

func (c *Controller) rpcError(err error) error {
	switch {
	case errors.Is(err, taskerrors.ErrInvalidToken):
		return status.Error(codes.Unauthenticated, taskerrors.ErrInvalidToken.Error())
	case errors.Is(err, taskerrors.ErrAuthUnavailable):
		c.logger.Error("task auth validation failed", "error", err)
		return status.Error(codes.Unavailable, taskerrors.ErrAuthUnavailable.Error())
	case errors.Is(err, taskerrors.ErrInvalidInput):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, taskerrors.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, taskerrors.ErrAlreadyDecided), errors.Is(err, taskerrors.ErrExternalConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		if ctxStatus := status.FromContextError(err); ctxStatus.Code() != codes.Unknown {
			return ctxStatus.Err()
		}
		c.logger.Error("tracker request failed", "error", err)
		return status.Error(codes.Internal, "internal server error")
	}
}

func toProto(request entity.AccessRequest) *taskv1.AccessRequest {
	response := &taskv1.AccessRequest{
		TaskId: request.ID, ProposalId: request.ProposalID, UserId: request.UserID, TeamId: request.TeamID,
		ResourceId: request.ResourceID, AccessLevel: request.AccessLevel, Status: statusToProto(request.Status),
		CreatedAt: timestamppb.New(request.CreatedAt), DecidedByUserId: request.DecidedByUserID,
		DecisionComment: request.DecisionComment, ExternalRequestId: request.ExternalRequestID,
	}
	if request.DecidedAt != nil {
		response.DecidedAt = timestamppb.New(*request.DecidedAt)
	}
	return response
}

func decisionStatus(status taskv1.AccessRequestStatus) entity.Status {
	switch status {
	case taskv1.AccessRequestStatus_ACCESS_REQUEST_STATUS_APPROVED:
		return entity.Approved
	case taskv1.AccessRequestStatus_ACCESS_REQUEST_STATUS_REJECTED:
		return entity.Rejected
	default:
		return ""
	}
}

func statusToProto(status entity.Status) taskv1.AccessRequestStatus {
	switch status {
	case entity.Pending:
		return taskv1.AccessRequestStatus_ACCESS_REQUEST_STATUS_PENDING
	case entity.Approved:
		return taskv1.AccessRequestStatus_ACCESS_REQUEST_STATUS_APPROVED
	case entity.Rejected:
		return taskv1.AccessRequestStatus_ACCESS_REQUEST_STATUS_REJECTED
	default:
		return taskv1.AccessRequestStatus_ACCESS_REQUEST_STATUS_UNSPECIFIED
	}
}
