package entity

import "errors"

var (
	ErrInvalidInput     = errors.New("invalid input")
	ErrMetadataNotFound = errors.New("internship not found")
	ErrTeamNotFound     = errors.New("team not found")
	ErrMentorNotInTeam  = errors.New("mentor is not a member of the team")
	ErrProfileConflict  = errors.New("profile already exists with different names")
	ErrInternshipExists = errors.New("internship already exists")
)

var ErrInvalidToken = errors.New("invalid access token")
var ErrAuthUnavailable = errors.New("auth unavailable")
