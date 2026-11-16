package entity

import "time"

// Conversation is a one-on-one dialog between the service and an intern.
type Conversation struct {
	ID        int64
	UserID    int64
	CreatedAt time.Time
}

type ConversationSummary struct {
	ID        int64
	Title     string
	CreatedAt time.Time
}
