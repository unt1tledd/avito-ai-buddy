package taskstatus

import (
	"context"
	"errors"
	"reflect"
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

func TestSaveTaskStatus(t *testing.T) {
	ctx := context.Background()
	conversations := &fakeConversations{id: 21}
	messages := &fakeMessages{}
	hub := &fakeHub{}
	service := New(zap.NewNop(), conversations, messages, hub)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", ExternalRequestID: "access-42", Status: "approved"}
	got, err := service.SaveTaskStatus(ctx, event)
	if err != nil || got == nil {
		t.Fatalf("SaveTaskStatus() = %+v, %v, want a saved message", got, err)
	}

	if len(messages.keys) != 1 || messages.keys[0] <= 0 {
		t.Fatalf("saved idempotency keys = %v, want a single positive key", messages.keys)
	}
	if len(hub.messages) != 0 {
		t.Fatal("saving a message must not publish it")
	}
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

func TestSaveTaskStatusSkipsRedeliveredEvents(t *testing.T) {
	ctx := context.Background()
	conversations := &fakeConversations{id: 21}
	messages := &fakeMessages{}
	hub := &fakeHub{}
	service := New(zap.NewNop(), conversations, messages, hub)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	if message, err := service.SaveTaskStatus(ctx, event); err != nil || message == nil {
		t.Fatalf("SaveTaskStatus() = %+v, %v, want a saved message", message, err)
	}
	if message, err := service.SaveTaskStatus(ctx, event); err != nil || message != nil {
		t.Fatalf("SaveTaskStatus() on redelivery = %+v, %v, want nil, nil", message, err)
	}

	if len(messages.keys) != 1 {
		t.Fatalf("saved messages = %d, want 1", len(messages.keys))
	}
}

func TestSaveTaskStatusKeepsStableKeyPerEvent(t *testing.T) {
	ctx := context.Background()
	conversations := &fakeConversations{id: 21}
	messages := &fakeMessages{}
	hub := &fakeHub{}
	service := New(zap.NewNop(), conversations, messages, hub)

	first := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	second := port.TaskEvent{UserID: "7", TaskID: "task-43", Status: "approved"}
	for _, event := range []port.TaskEvent{first, second, first} {
		if _, err := service.SaveTaskStatus(ctx, event); err != nil {
			t.Fatalf("SaveTaskStatus(%s) = %v, want nil", event.TaskID, err)
		}
	}

	// Same event key maps to the same idempotency key, different events differ.
	if len(messages.keys) != 2 || messages.keys[0] == messages.keys[1] {
		t.Fatalf("saved keys = %v, want two distinct keys", messages.keys)
	}
}

func TestSaveTaskStatusRejectsInvalidUser(t *testing.T) {
	ctx := context.Background()
	service := New(zap.NewNop(), &fakeConversations{}, &fakeMessages{}, &fakeHub{})

	for _, userID := range []string{"", "abc", "0", "-7"} {
		if _, err := service.SaveTaskStatus(ctx, port.TaskEvent{UserID: userID, TaskID: "task-42", Status: "approved"}); err == nil {
			t.Fatalf("SaveTaskStatus(user %q) = nil, want an error", userID)
		}
	}
}

func TestPublishSavedTaskStatus(t *testing.T) {
	ctx := context.Background()
	conversations := &fakeConversations{id: 21}
	messages := &fakeMessages{}
	hub := &fakeHub{}
	service := New(zap.NewNop(), conversations, messages, hub)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	message, err := service.SaveTaskStatus(ctx, event)
	if err != nil || message == nil {
		t.Fatalf("SaveTaskStatus() = %+v, %v, want a saved message", message, err)
	}
	service.PublishSavedTaskStatus(ctx, event, *message)
	if len(hub.messages) != 1 || !reflect.DeepEqual(hub.messages[0], *message) {
		t.Fatalf("hub messages = %+v, want %+v", hub.messages, *message)
	}

	hub.err = errors.New("no live subscribers")
	service.PublishSavedTaskStatus(ctx, event, *message)
	if len(messages.keys) != 1 {
		t.Fatalf("saved messages = %d, want 1 despite the failed push", len(messages.keys))
	}
}

func TestSaveTaskStatusPropagatesStorageFailure(t *testing.T) {
	ctx := context.Background()
	dbErr := errors.New("database unavailable")
	conversations := &fakeConversations{}
	messages := &fakeMessages{err: dbErr}
	service := New(zap.NewNop(), conversations, messages, &fakeHub{})

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	if _, err := service.SaveTaskStatus(ctx, event); !errors.Is(err, dbErr) {
		t.Fatalf("SaveTaskStatus() = %v, want %v", err, dbErr)
	}
}
