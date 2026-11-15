package entity

import "time"

// Action is an access proposal attached to an assistant message: the intern
// may confirm it, which sends an access request to the task service.
type Action struct {
	ID            int64
	MessageID     int64
	ResourceID    string
	ResourceLabel string
	AccessLevel   string
	CreatedAt     time.Time
	AccessRequest *AccessRequest
}

// AccessRequest is the dispatch state of a confirmed proposal: it is tracked
// until the task service reports the request outcome.
type AccessRequest struct {
	TaskID            string
	ExternalRequestID string
	Status            string
}
