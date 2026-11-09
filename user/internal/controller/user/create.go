package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	userv1 "avito-ai-buddy/pkg/generated/user/v1"
	"avito-ai-buddy/user/internal/entity"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *userServer) Create(ctx context.Context, req *userv1.CreateRequest) (*userv1.CreateResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	start, err := parseDate("start_date", req.GetStartDate())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	plannedEnd, err := parseDate("planned_end_date", req.GetPlannedEndDate())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	var actualEnd *time.Time
	if req.ActualEndDate != nil {
		date, err := parseDate("actual_end_date", req.GetActualEndDate())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%v", err)
		}
		actualEnd = &date
	}
	profile := entity.Profile{
		UserID:     req.GetUserId(),
		LastName:   req.GetLastName(),
		FirstName:  req.GetFirstName(),
		MiddleName: req.MiddleName,
	}
	internship := entity.Internship{
		TeamID:         req.GetTeamId(),
		MentorUserID:   req.MentorUserId,
		Track:          req.GetTrack(),
		StartDate:      start,
		PlannedEndDate: plannedEnd,
		ActualEndDate:  actualEnd,
	}
	id, err := s.service.Create(ctx, profile, internship)
	if err != nil {
		switch {
		case errors.Is(err, entity.ErrInvalidInput), errors.Is(err, entity.ErrMentorNotInTeam):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		case errors.Is(err, entity.ErrTeamNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		case errors.Is(err, entity.ErrProfileConflict), errors.Is(err, entity.ErrInternshipExists):
			return nil, status.Error(codes.AlreadyExists, err.Error())
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil, status.FromContextError(err).Err()
		default:
			s.logger.Error("failed to create user internship", zap.Int64("user_id", profile.UserID), zap.Error(err))
			return nil, status.Error(codes.Internal, "internal server error")
		}
	}
	return &userv1.CreateResponse{InternshipId: id}, nil
}

func parseDate(field, value string) (time.Time, error) {
	date, err := time.Parse(time.DateOnly, value)
	if err != nil || date.Year() == 0 {
		return time.Time{}, fmt.Errorf("%w: %s must be a date in YYYY-MM-DD format", entity.ErrInvalidInput, field)
	}
	return date, nil
}
