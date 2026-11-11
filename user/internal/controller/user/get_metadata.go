package user

import (
	"context"
	"errors"
	"time"

	userv1 "avito-ai-buddy/pkg/generated/user/v1"
	"avito-ai-buddy/user/internal/entity"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *userServer) GetMetadata(ctx context.Context, req *userv1.GetMetadataRequest) (*userv1.GetMetadataResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	userID := req.GetUserId()
	metadata, err := s.service.GetMetadata(ctx, userID)
	if err != nil {
		switch {
		case errors.Is(err, entity.ErrInvalidInput):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		case errors.Is(err, entity.ErrMetadataNotFound):
			return nil, status.Error(codes.NotFound, entity.ErrMetadataNotFound.Error())
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil, status.FromContextError(err).Err()
		default:
			s.logger.Error("failed to get user metadata", zap.Int64("user_id", userID), zap.Error(err))
			return nil, status.Error(codes.Internal, "internal server error")
		}
	}

	response := &userv1.GetMetadataResponse{
		InternshipId: metadata.InternshipID,
		Intern: &userv1.GetMetadataResponse_Profile{
			UserId:     metadata.Intern.UserID,
			LastName:   metadata.Intern.LastName,
			FirstName:  metadata.Intern.FirstName,
			MiddleName: metadata.Intern.MiddleName,
		},
		Team: &userv1.GetMetadataResponse_Team{
			TeamId: metadata.Team.ID,
			Name:   metadata.Team.Name,
		},
		Track:               metadata.Internship.Track,
		StartDate:           metadata.Internship.StartDate.Format(time.DateOnly),
		PlannedEndDate:      metadata.Internship.PlannedEndDate.Format(time.DateOnly),
		PlannedDurationDays: metadata.PlannedDurationDays,
		ActualDurationDays:  metadata.ActualDurationDays,
	}
	if mentor := metadata.Mentor; mentor != nil {
		response.Mentor = &userv1.GetMetadataResponse_Profile{
			UserId:     mentor.UserID,
			LastName:   mentor.LastName,
			FirstName:  mentor.FirstName,
			MiddleName: mentor.MiddleName,
		}
	}
	if metadata.Internship.ActualEndDate != nil {
		date := metadata.Internship.ActualEndDate.Format(time.DateOnly)
		response.ActualEndDate = &date
	}
	return response, nil
}
