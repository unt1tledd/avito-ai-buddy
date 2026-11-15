package task

import (
	"context"
	"io"
	"log/slog"
	"testing"

	taskv1 "avito-ai-buddy/pkg/generated/task/v1"
	"avito-ai-buddy/task/internal/entity"
	taskerrors "avito-ai-buddy/task/internal/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type serviceStub struct {
	list   func(int64, int64, int32) ([]entity.AccessRequest, error)
	decide func(entity.Decision) (entity.AccessRequest, error)
}

func (s serviceStub) ListPending(_ context.Context, teamID, afterID int64, limit int32) ([]entity.AccessRequest, error) {
	return s.list(teamID, afterID, limit)
}

func (s serviceStub) GetUserTasks(_ context.Context, userID, afterID int64, limit int32) ([]entity.AccessRequest, error) {
	return s.list(userID, afterID, limit)
}

func (s serviceStub) Decide(_ context.Context, decision entity.Decision) (entity.AccessRequest, error) {
	return s.decide(decision)
}

func TestControllerAuthorizationAndValidation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	controller := NewController(nil, logger, "test-token", nil, 50)
	auth := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer test-token"))

	if _, err := controller.ListPending(t.Context(), &taskv1.ListPendingRequest{TeamId: 7}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("ListPending without token: %v", err)
	}
	wrongToken := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer invalid"))
	if _, err := controller.Decide(wrongToken, &taskv1.DecideRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Decide with wrong token: %v", err)
	}

	if _, err := controller.ListPending(auth, &taskv1.ListPendingRequest{TeamId: 7, Limit: new(int32)}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListPending with zero limit: %v", err)
	}
	if _, err := controller.Decide(auth, &taskv1.DecideRequest{TaskId: 305}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Decide without decision: %v", err)
	}
	invalid := &taskv1.DecideRequest{TaskId: 305, Decision: &taskv1.Decision{
		TeamId: 7, DecidedByUserId: 51, Status: taskv1.AccessRequestStatus_ACCESS_REQUEST_STATUS_PENDING,
	}}
	if _, err := controller.Decide(auth, invalid); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Decide with pending status: %v", err)
	}
}

func TestListPendingUsesDefaultLimitAndEncodesStatus(t *testing.T) {
	tasks := serviceStub{list: func(teamID, afterID int64, limit int32) ([]entity.AccessRequest, error) {
		if teamID != 7 || afterID != 305 || limit != 20 {
			t.Fatalf("unexpected pagination: %d, %d, %d", teamID, afterID, limit)
		}
		return []entity.AccessRequest{{ID: 306, Status: entity.Pending}}, nil
	}}
	controller := NewController(tasks, slog.Default(), "test-token", nil, 20)
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer test-token"))
	got, err := controller.ListPending(ctx, &taskv1.ListPendingRequest{TeamId: 7, AfterId: 305})
	if err != nil || len(got.GetTasks()) != 1 ||
		got.GetTasks()[0].GetTaskId() != 306 ||
		got.GetTasks()[0].GetStatus() != taskv1.AccessRequestStatus_ACCESS_REQUEST_STATUS_PENDING {
		t.Fatalf("ListPending: %v, %v", got, err)
	}
}

func TestDecidePassesDecisionAndMapsConflict(t *testing.T) {
	tasks := serviceStub{decide: func(got entity.Decision) (entity.AccessRequest, error) {
		want := entity.Decision{TaskID: 305, TeamID: 7, DecidedByUserID: 51, Status: entity.Approved, ExternalRequestID: "ext-1"}
		if got != want {
			t.Fatalf("decision=%+v, want %+v", got, want)
		}
		return entity.AccessRequest{}, taskerrors.ErrAlreadyDecided
	}}
	controller := NewController(tasks, slog.Default(), "test-token", nil, 50)
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer test-token"))
	_, err := controller.Decide(ctx, &taskv1.DecideRequest{TaskId: 305, Decision: &taskv1.Decision{
		TeamId: 7, DecidedByUserId: 51, Status: taskv1.AccessRequestStatus_ACCESS_REQUEST_STATUS_APPROVED, ExternalRequestId: "ext-1",
	}})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("Decide: %v, want AlreadyExists", err)
	}
}

type tokenValidatorStub func(context.Context, string) (int64, error)

func (f tokenValidatorStub) ValidateToken(ctx context.Context, token string) (int64, error) {
	return f(ctx, token)
}

func TestGetUserTasksRequiresOwnerAndUsesSessionUser(t *testing.T) {
	auth := tokenValidatorStub(func(_ context.Context, token string) (int64, error) {
		if token != "intern-token" {
			return 0, taskerrors.ErrInvalidToken
		}
		return 42, nil
	})
	tasks := serviceStub{list: func(userID, afterID int64, limit int32) ([]entity.AccessRequest, error) {
		if userID != 42 || afterID != 7 || limit != 50 {
			t.Fatalf("unexpected user or pagination: %d, %d, %d", userID, afterID, limit)
		}
		return []entity.AccessRequest{{ID: 8, UserID: userID, Status: entity.Approved}}, nil
	}}
	controller := NewController(tasks, slog.Default(), "test-token", auth, 50)
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer intern-token"))

	got, err := controller.GetUserTasks(ctx, &taskv1.GetUserTasksRequest{UserId: 42, AfterId: 7})
	if err != nil || len(got.GetTasks()) != 1 ||
		got.GetTasks()[0].GetStatus() != taskv1.AccessRequestStatus_ACCESS_REQUEST_STATUS_APPROVED {
		t.Fatalf("GetUserTasks for owner: %v, %v", got, err)
	}
	if _, err := controller.GetUserTasks(ctx, &taskv1.GetUserTasksRequest{UserId: 43}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("GetUserTasks for another user: %v", err)
	}
	if _, err := controller.GetUserTasks(t.Context(), &taskv1.GetUserTasksRequest{UserId: 42}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("GetUserTasks without a token: %v", err)
	}
	tracker := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer test-token"))
	if _, err := controller.GetUserTasks(tracker, &taskv1.GetUserTasksRequest{UserId: 42}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("GetUserTasks with tracker credentials: %v", err)
	}
}
