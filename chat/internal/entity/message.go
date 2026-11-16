package entity

import "time"

// Message is a single entry of a conversation: a user question, an assistant
// reply or a task status notification posted as an assistant message.
type Message struct {
	ID             int64
	ConversationID int64
	Role           MessageRole
	Text           string
	CreatedAt      time.Time
	Sources        []Source
	Proposal       *Action
}

// HistoryPage is one page of a conversation history with a cursor to fetch
// the next page.
type HistoryPage struct {
	Messages      []Message
	NextPageToken string
}

// MessageRole distinguishes who authored a message.
type MessageRole string

const (
	MessageRoleUnspecified MessageRole = "unspecified"
	MessageRoleUser        MessageRole = "user"
	MessageRoleAssistant   MessageRole = "assistant"
)

// Source is a document referenced by an assistant reply.
type Source struct {
	DocumentID string
	Title      string
	URL        string
}
