package errors

import "errors"

var (
	ErrInvalidInput     = errors.New("invalid task input")
	ErrNotFound         = errors.New("access request not found")
	ErrProposalConflict = errors.New("proposal already used for a different access request")
	ErrExternalConflict = errors.New("external request ID already used by another task")
	ErrAlreadyDecided   = errors.New("access request already decided")
)
