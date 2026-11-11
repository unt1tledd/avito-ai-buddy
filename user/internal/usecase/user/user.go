package user

import (
	"context"
	"fmt"
	"strings"

	"avito-ai-buddy/user/internal/entity"
)

type userRepository interface {
	Create(ctx context.Context, profile entity.Profile, internship entity.Internship) (int64, error)
	GetMetadata(ctx context.Context, userID int64) (entity.Metadata, error)
}

type Service struct {
	repository userRepository
}

func NewUserService(repository userRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, profile entity.Profile, internship entity.Internship) (int64, error) {
	profile.FirstName = strings.TrimSpace(profile.FirstName)
	profile.LastName = strings.TrimSpace(profile.LastName)
	if profile.MiddleName != nil {
		middleName := strings.TrimSpace(*profile.MiddleName)
		profile.MiddleName = nil
		if middleName != "" {
			profile.MiddleName = &middleName
		}
	}
	internship.Track = strings.TrimSpace(internship.Track)

	if profile.UserID <= 0 || internship.TeamID <= 0 {
		return 0, fmt.Errorf("%w: user_id and team_id must be positive", entity.ErrInvalidInput)
	}
	if profile.FirstName == "" || profile.LastName == "" || internship.Track == "" {
		return 0, fmt.Errorf("%w: first_name, last_name and track are required", entity.ErrInvalidInput)
	}
	if internship.MentorUserID != nil && (*internship.MentorUserID <= 0 || *internship.MentorUserID == profile.UserID) {
		return 0, fmt.Errorf("%w: mentor_user_id must identify a different user", entity.ErrInvalidInput)
	}
	if internship.StartDate.IsZero() || internship.PlannedEndDate.IsZero() {
		return 0, fmt.Errorf("%w: start_date and planned_end_date are required", entity.ErrInvalidInput)
	}
	if internship.PlannedEndDate.Before(internship.StartDate) ||
		(internship.ActualEndDate != nil && internship.ActualEndDate.Before(internship.StartDate)) {
		return 0, fmt.Errorf("%w: end dates must be on or after start_date", entity.ErrInvalidInput)
	}

	return s.repository.Create(ctx, profile, internship)
}

func (s *Service) GetMetadata(ctx context.Context, userID int64) (entity.Metadata, error) {
	if userID <= 0 {
		return entity.Metadata{}, fmt.Errorf("%w: user_id must be positive", entity.ErrInvalidInput)
	}
	return s.repository.GetMetadata(ctx, userID)
}
