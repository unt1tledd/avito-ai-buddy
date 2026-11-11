package entity

// InboxMessage is a marker of an already processed incoming event: the
// idempotency key is stored in one transaction with the business effect of
// the event, so redeliveries are skipped.
type InboxMessage struct {
	IdempotencyKey string
	Data           []byte
	Kind           int32
}
