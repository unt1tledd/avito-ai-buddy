package errors

import "errors"

var (
	ErrInvalidToken         = errors.New("invalid or expired access token")
	ErrAuthUnavailable      = errors.New("auth service unavailable")
	ErrInvalidUserID        = errors.New("user ID must be positive")
	ErrInvalidStepID        = errors.New("step ID is required")
	ErrUnsupportedTrack     = errors.New("unsupported onboarding track")
	ErrPlanTrackConflict    = errors.New("a plan for another track already exists")
	ErrPlanNotFound         = errors.New("onboarding plan not found")
	ErrStepNotFound         = errors.New("step not found in the user's plan")
	ErrStepTemplateNotFound = errors.New("onboarding step template not found")
	ErrUserMetadataNotFound = errors.New("user internship metadata not found")
	ErrUserUnavailable      = errors.New("user service unavailable")
	ErrInvalidUserMetadata  = errors.New("user service returned invalid internship metadata")
)
