package entity

import "time"

type Message struct {
	ID             int64
	ConversationID int64
	Role           MessageRole
	Text           string
	CreatedAt      time.Time
	Sources        []Source
	Proposal       *Action
}

type HistoryPage struct {
	Messages      []Message
	NextPageToken string
}

type MessageRole string

const (
	MessageRoleUnspecified MessageRole = "unspecified"
	MessageRoleUser        MessageRole = "user"
	MessageRoleAssistant   MessageRole = "assistant"
)

type Source struct {
	DocumentID string
	Title      string
	URL        string
}
