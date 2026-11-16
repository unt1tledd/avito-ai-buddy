package errors

import "errors"

var (
	ErrAuthUnavailable        = errors.New("auth service unavailable")
	ErrInvalidToken           = errors.New("invalid or expired access token")
	ErrMissingConfirmationIDs = errors.New("user and proposal IDs are required")
	ErrProposalNotFound       = errors.New("proposal not found")
	//nolint:staticcheck // The gRPC API displays this message to the user.
	ErrProposalExpired = errors.New("Срок предложения истёк. Попросите помощника создать новое")
)

var (
	ErrInvalidMessageLength = errors.New("invalid message length")
	ErrGetConversation      = errors.New("get chat conversation")
	ErrListMessages         = errors.New("list conversation messages")
	ErrGetUserContext       = errors.New("get user context")
	ErrGetOnboardingPlans   = errors.New("get onboarding plans")
	ErrGetAIAnswer          = errors.New("get AI answer")
	ErrSaveUserMessage      = errors.New("save user message")
	ErrSaveAssistantMessage = errors.New("save assistant message")
	ErrSaveProposal         = errors.New("save proposal")
	ErrIdempotencyKeyReused = errors.New("idempotency key was already used for different text")
	ErrReplyInProgress      = errors.New("reply is in progress, retry with the same idempotency key")
)
