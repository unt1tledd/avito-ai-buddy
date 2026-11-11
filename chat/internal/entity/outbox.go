package entity

// Outbox message statuses.
const (
	OutboxStatusCreated    = "CREATED"
	OutboxStatusInProgress = "IN_PROGRESS"
	OutboxStatusSuccess    = "SUCCESS"
	OutboxStatusFailed     = "FAILED"
)

const OutboxKindCreateAccessRequest int32 = 1

type OutboxMessage struct {
	IdempotencyKey string
	Data           []byte
	Kind           int32
	Attempts       int32
}
