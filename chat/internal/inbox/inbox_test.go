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

// recordingTx mimics a real transaction: when fn fails, the changes made
// inside it are rolled back.
type recordingTx struct {
	queue *fakeQueue
}

func (tx *recordingTx) WithTx(ctx context.Context, fn func(context.Context) error) error {
	tx.queue.mu.Lock()
	before := make(map[string]struct{}, len(tx.queue.seen))
	for key := range tx.queue.seen {
		before[key] = struct{}{}
	}
	tx.queue.mu.Unlock()

	err := fn(ctx)
	if err != nil {
		tx.queue.mu.Lock()
		defer tx.queue.mu.Unlock()
		for key := range tx.queue.seen {
			if _, ok := before[key]; !ok {
				delete(tx.queue.seen, key)
			}
		}
	}
	return err
}

type fakeNotifier struct {
	calls  []port.TaskEvent
	err    error
	called int
}

func (n *fakeNotifier) PublishTaskStatus(_ context.Context, event port.TaskEvent) error {
	n.called++
	if n.err != nil {
		return n.err
	}
	n.calls = append(n.calls, event)
	return nil
}

func TestFirstDeliveryIsProcessed(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{}
	notifier := &fakeNotifier{}
	inbox := New(queue, &recordingTx{queue: queue}, notifier)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", ExternalRequestID: "access-42", Status: "approved"}
	if err := inbox.PublishTaskStatus(ctx, event); err != nil {
		t.Fatalf("PublishTaskStatus() = %v, want nil", err)
	}
	if notifier.called != 1 || len(notifier.calls) != 1 || notifier.calls[0] != event {
		t.Fatalf("notifier calls = %+v, want single delivery of %+v", notifier.calls, event)
	}
	if len(queue.insert) != 1 || queue.insert[0].IdempotencyKey != "access-42" {
		t.Fatalf("inserted keys = %+v, want [access-42]", queue.insert)
	}
}

func TestRedeliveredEventIsSkipped(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{}
	notifier := &fakeNotifier{}
	inbox := New(queue, &recordingTx{queue: queue}, notifier)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	for i := 0; i < 3; i++ {
		if err := inbox.PublishTaskStatus(ctx, event); err != nil {
			t.Fatalf("PublishTaskStatus() #%d = %v, want nil", i+1, err)
		}
	}

	if notifier.called != 1 {
		t.Fatalf("notifier called %d times, want 1 (duplicates must be skipped)", notifier.called)
	}
	if key := queue.insert[0].IdempotencyKey; key != "task-42|approved" {
		t.Fatalf("idempotency key = %q, want task-42|approved", key)
	}
}

func TestFailedDeliveryDoesNotMarkEventAsProcessed(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{}
	notifier := &fakeNotifier{err: errors.New("hub unavailable")}
	inbox := New(queue, &recordingTx{queue: queue}, notifier)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	if err := inbox.PublishTaskStatus(ctx, event); err == nil {
		t.Fatal("PublishTaskStatus() = nil, want delivery error")
	}

	notifier.err = nil
	if err := inbox.PublishTaskStatus(ctx, event); err != nil {
		t.Fatalf("retry after failed delivery = %v, want nil", err)
	}
	if notifier.called != 2 {
		t.Fatalf("notifier called %d times, want 2", notifier.called)
	}
}

func TestQueueFailurePropagates(t *testing.T) {
	ctx := context.Background()
	dbErr := errors.New("database unavailable")
	queue := &fakeQueue{fail: dbErr}
	inbox := New(queue, &recordingTx{queue: queue}, &fakeNotifier{})

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	if err := inbox.PublishTaskStatus(ctx, event); !errors.Is(err, dbErr) {
		t.Fatalf("PublishTaskStatus() = %v, want %v", err, dbErr)
	}
}
