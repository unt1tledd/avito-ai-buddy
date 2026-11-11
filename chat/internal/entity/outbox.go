package entity

const OutboxKindCreateAccessRequest int32 = 1

type OutboxMessage struct {
	IdempotencyKey string
	Data           []byte
	Kind           int32
	Attempts       int32
}
