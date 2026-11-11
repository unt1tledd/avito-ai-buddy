package inbox

import (
	"context"
	"errors"
	"sync"
	"testing"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/port"
)

type fakeQueue struct {
	mu     sync.Mutex
	seen   map[string]struct{}
	insert []entity.InboxMessage
	delete []string
	fail   error
}

func (q *fakeQueue) TryInsert(_ context.Context, message entity.InboxMessage) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.fail != nil {
		return false, q.fail
	}
	if _, ok := q.seen[message.IdempotencyKey]; ok {
		return false, nil
	}
	if q.seen == nil {
		q.seen = make(map[string]struct{})
	}
	q.seen[message.IdempotencyKey] = struct{}{}
	q.insert = append(q.insert, message)
	return true, nil
}

func (q *fakeQueue) Delete(_ context.Context, idempotencyKey string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.seen, idempotencyKey)
	q.delete = append(q.delete, idempotencyKey)
	return nil
}

type fakeNotifier struct {
	events []port.TaskEvent
	err    error
	calls  int
}

func (n *fakeNotifier) PublishTaskStatus(_ context.Context, event port.TaskEvent) error {
	n.calls++
	if n.err != nil {
		return n.err
	}
	n.events = append(n.events, event)
	return nil
}

func TestFirstDeliveryIsProcessed(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{}
	notifier := &fakeNotifier{}
	inbox := New(queue, notifier)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", ExternalRequestID: "access-42", Status: "approved"}
	if err := inbox.PublishTaskStatus(ctx, event); err != nil {
		t.Fatalf("PublishTaskStatus() = %v, want nil", err)
	}
	if notifier.calls != 1 || len(notifier.events) != 1 || notifier.events[0] != event {
		t.Fatalf("notifier deliveries = %+v, want single delivery of %+v", notifier.events, event)
	}
	if len(queue.insert) != 1 || queue.insert[0].IdempotencyKey != "access-42" {
		t.Fatalf("inserted keys = %+v, want [access-42]", queue.insert)
	}
}

func TestRedeliveredEventIsSkipped(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{}
	notifier := &fakeNotifier{}
	inbox := New(queue, notifier)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	for i := 0; i < 3; i++ {
		if err := inbox.PublishTaskStatus(ctx, event); err != nil {
			t.Fatalf("PublishTaskStatus() #%d = %v, want nil", i+1, err)
		}
	}

	if notifier.calls != 1 {
		t.Fatalf("notifier called %d times, want 1 (duplicates must be skipped)", notifier.calls)
	}
	if key := queue.insert[0].IdempotencyKey; key != "task-42|approved" {
		t.Fatalf("idempotency key = %q, want task-42|approved", key)
	}
}

func TestFailedDeliveryRollsMarkerBack(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{}
	notifier := &fakeNotifier{err: errors.New("hub unavailable")}
	inbox := New(queue, notifier)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	if err := inbox.PublishTaskStatus(ctx, event); err == nil {
		t.Fatal("PublishTaskStatus() = nil, want delivery error")
	}
	if len(queue.delete) != 1 || queue.delete[0] != "task-42|approved" {
		t.Fatalf("rolled back keys = %v, want [task-42|approved]", queue.delete)
	}

	// The next redelivery processes the event again.
	notifier.err = nil
	if err := inbox.PublishTaskStatus(ctx, event); err != nil {
		t.Fatalf("retry after failed delivery = %v, want nil", err)
	}
	if notifier.calls != 2 {
		t.Fatalf("notifier called %d times, want 2", notifier.calls)
	}
}

func TestQueueFailurePropagates(t *testing.T) {
	ctx := context.Background()
	dbErr := errors.New("database unavailable")
	queue := &fakeQueue{fail: dbErr}
	inbox := New(queue, &fakeNotifier{})

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	if err := inbox.PublishTaskStatus(ctx, event); !errors.Is(err, dbErr) {
		t.Fatalf("PublishTaskStatus() = %v, want %v", err, dbErr)
	}
}
