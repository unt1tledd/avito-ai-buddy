package entity

import "time"

type Status string

const (
	Pending  Status = "pending"
	Approved Status = "approved"
	Rejected Status = "rejected"
)

type CreateRequest struct {
	ProposalID  int64
	UserID      int64
	ResourceID  string
	AccessLevel string
}

type AccessRequest struct {
	ID                int64      `json:"task_id,string"`
	ProposalID        int64      `json:"proposal_id,string"`
	UserID            int64      `json:"user_id,string"`
	TeamID            int64      `json:"team_id,string"`
	ResourceID        string     `json:"resource_id"`
	AccessLevel       string     `json:"access_level"`
	Status            Status     `json:"status"`
	CreatedAt         time.Time  `json:"created_at"`
	DecidedAt         *time.Time `json:"decided_at,omitempty"`
	DecidedByUserID   *int64     `json:"decided_by_user_id,string,omitempty"`
	DecisionComment   string     `json:"decision_comment,omitempty"`
	ExternalRequestID string     `json:"external_request_id,omitempty"`
}

type Decision struct {
	TaskID            int64  `json:"-"`
	TeamID            int64  `json:"team_id,string"`
	DecidedByUserID   int64  `json:"decided_by_user_id,string"`
	Status            Status `json:"status"`
	Comment           string `json:"comment"`
	ExternalRequestID string `json:"external_request_id"`
}

// StatusEvent is the JSON contract consumed by Chat.
type StatusEvent struct {
	ProposalID        int64  `json:"proposal_id,string"`
	UserID            int64  `json:"user_id,string"`
	TaskID            int64  `json:"task_id,string"`
	Status            Status `json:"status"`
	ExternalRequestID string `json:"external_request_id,omitempty"`
}
