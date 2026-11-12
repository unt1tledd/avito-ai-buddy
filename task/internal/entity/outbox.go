package entity

type OutboxMessage struct {
	TaskID   int64
	Payload  []byte
	Attempts int32
	Failed   bool
}
