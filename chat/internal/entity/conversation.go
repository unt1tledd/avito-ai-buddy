package entity

import "time"

type Conversation struct {
	ID        int64
	UserID    int64
	CreatedAt time.Time
}
