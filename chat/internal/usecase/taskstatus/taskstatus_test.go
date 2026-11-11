package taskstatus

import (
	"context"
	"errors"
	"testing"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/port"

	"go.uber.org/zap"
)

type fakeConversations struct {
	id  int64
	err error
}

func (c *fakeConversations) GetOrCreate(_ context.Context, userID int64) (*entity.Conversation, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &entity.Conversation{ID: c.id, UserID: userID}, nil
}

type fakeMessages struct {
	keys  []int64
	err   error
	dupes map[int64]struct{}
}

func (m *fakeMessages) CreateIdempotent(_ context.Context, _ entity.Message, idempotencyKey int64) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	if _, ok := m.dupes[idempotencyKey]; ok {
		return false, nil
	}
	if m.dupes == nil {
		m.dupes = make(map[int64]struct{})
	}
	m.dupes[idempotencyKey] = struct{}{}
	m.keys = append(m.keys, idempotencyKey)
	return true, nil
}

type fakeHub struct {
	messages []entity.Message
	err      error
}

func (h *fakeHub) PublishMessage(_ context.Context, _ string, message entity.Message) error {
	if h.err != nil {
		return h.err
	}
	h.messages = append(h.messages, message)
	return nil
}

func TestPublishTaskStatusSavesMessageAndPushes(t *testing.T) {
	ctx := context.Background()
	conversations := &fakeConversations{id: 21}
	messages := &fakeMessages{}
	hub := &fakeHub{}
	service := New(zap.NewNop(), conversations, messages, hub)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", ExternalRequestID: "access-42", Status: "approved"}
	if err := service.PublishTaskStatus(ctx, event); err != nil {
		t.Fatalf("PublishTaskStatus() = %v, want nil", err)
	}

	if len(messages.keys) != 1 || messages.keys[0] <= 0 {
		t.Fatalf("saved idempotency keys = %v, want a single positive key", messages.keys)
	}
	if len(hub.messages) != 1 {
		t.Fatalf("hub deliveries = %d, want 1", len(hub.messages))
	}
	got := hub.messages[0]
	if got.ConversationID != 21 || got.Role != entity.MessageRoleAssistant {
		t.Fatalf("message = %+v, want an assistant message in conversation 21", got)
	}
	if got.Text != "Статус задачи task-42 изменён: approved." {
		t.Fatalf("message text = %q, want the status update", got.Text)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("message has no creation timestamp")
	}
}

func TestPublishTaskStatusSkipsRedeliveredEvents(t *testing.T) {
	ctx := context.Background()
	conversations := &fakeConversations{id: 21}
	messages := &fakeMessages{}
	hub := &fakeHub{}
	service := New(zap.NewNop(), conversations, messages, hub)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	for i := 0; i < 2; i++ {
		if err := service.PublishTaskStatus(ctx, event); err != nil {
			t.Fatalf("PublishTaskStatus() #%d = %v, want nil", i+1, err)
		}
	}

	// The second delivery is deduplicated by the message idempotency key:
	// neither a second message nor a second push is produced.
	if len(messages.keys) != 1 {
		t.Fatalf("saved messages = %d, want 1", len(messages.keys))
	}
	if len(hub.messages) != 1 {
		t.Fatalf("hub deliveries = %d, want 1", len(hub.messages))
	}
}

func TestPublishTaskStatusKeepsStableKeyPerEvent(t *testing.T) {
	ctx := context.Background()
	conversations := &fakeConversations{id: 21}
	messages := &fakeMessages{}
	hub := &fakeHub{}
	service := New(zap.NewNop(), conversations, messages, hub)

	first := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	second := port.TaskEvent{UserID: "7", TaskID: "task-43", Status: "approved"}
	for _, event := range []port.TaskEvent{first, second, first} {
		if err := service.PublishTaskStatus(ctx, event); err != nil {
			t.Fatalf("PublishTaskStatus(%s) = %v, want nil", event.TaskID, err)
		}
	}

	// Same event key maps to the same idempotency key, different events differ.
	if len(messages.keys) != 2 || messages.keys[0] == messages.keys[1] {
		t.Fatalf("saved keys = %v, want two distinct keys", messages.keys)
	}
}

func TestPublishTaskStatusRejectsInvalidUser(t *testing.T) {
	ctx := context.Background()
	service := New(zap.NewNop(), &fakeConversations{}, &fakeMessages{}, &fakeHub{})

	for _, userID := range []string{"", "abc", "0", "-7"} {
		if err := service.PublishTaskStatus(ctx, port.TaskEvent{UserID: userID, TaskID: "task-42", Status: "approved"}); err == nil {
			t.Fatalf("PublishTaskStatus(user %q) = nil, want an error", userID)
		}
	}
}

func TestPublishTaskStatusKeepsMessageWhenPushFails(t *testing.T) {
	ctx := context.Background()
	conversations := &fakeConversations{id: 21}
	messages := &fakeMessages{}
	hub := &fakeHub{err: errors.New("no live subscribers")}
	service := New(zap.NewNop(), conversations, messages, hub)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	if err := service.PublishTaskStatus(ctx, event); err != nil {
		t.Fatalf("PublishTaskStatus() = %v, want nil: a failed push must not lose the message", err)
	}
	if len(messages.keys) != 1 {
		t.Fatalf("saved messages = %d, want 1 despite the failed push", len(messages.keys))
	}
}

func TestPublishTaskStatusPropagatesStorageFailure(t *testing.T) {
	ctx := context.Background()
	dbErr := errors.New("database unavailable")
	conversations := &fakeConversations{}
	messages := &fakeMessages{err: dbErr}
	service := New(zap.NewNop(), conversations, messages, &fakeHub{})

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	if err := service.PublishTaskStatus(ctx, event); !errors.Is(err, dbErr) {
		t.Fatalf("PublishTaskStatus() = %v, want %v", err, dbErr)
	}
}
