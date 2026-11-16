package inbox

import (
	"context"
	"errors"
	"testing"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/port"
)

type fakeQueue struct {
	seen   map[string]struct{}
	insert []entity.InboxMessage
	fail   error
}

func (q *fakeQueue) TryInsert(_ context.Context, message entity.InboxMessage) (bool, error) {
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

type transactorFunc func(context.Context, func(context.Context) error) error

func (f transactorFunc) WithTx(ctx context.Context, fn func(context.Context) error) error {
	return f(ctx, fn)
}

func runTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeTaskStatus struct {
	events    []port.TaskEvent
	published []entity.Message
	saveErr   error
	onPublish func()
}

func (n *fakeTaskStatus) SaveTaskStatus(_ context.Context, event port.TaskEvent) (*entity.Message, error) {
	if n.saveErr != nil {
		return nil, n.saveErr
	}
	n.events = append(n.events, event)
	return &entity.Message{Text: string(event.Status)}, nil
}

func (n *fakeTaskStatus) PublishSavedTaskStatus(_ context.Context, _ port.TaskEvent, message entity.Message) {
	if n.onPublish != nil {
		n.onPublish()
	}
	n.published = append(n.published, message)
}

func TestFirstDeliveryIsProcessed(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{}
	committed := false
	notifier := &fakeTaskStatus{onPublish: func() {
		if !committed {
			t.Fatal("published before the transaction committed")
		}
	}}
	tx := transactorFunc(func(ctx context.Context, fn func(context.Context) error) error {
		if err := fn(ctx); err != nil {
			return err
		}
		if len(queue.insert) != 1 || len(notifier.events) != 1 {
			t.Fatal("inbox marker and message must be saved before commit")
		}
		committed = true
		return nil
	})
	inbox := New(queue, notifier, tx)

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", ExternalRequestID: "access-42", Status: "approved"}
	if err := inbox.PublishTaskStatus(ctx, event); err != nil {
		t.Fatalf("PublishTaskStatus() = %v, want nil", err)
	}
	if notifier.events[0] != event || len(notifier.published) != 1 || notifier.published[0].Text != string(event.Status) {
		t.Fatalf("saved events = %+v, published messages = %+v", notifier.events, notifier.published)
	}
	if len(queue.insert) != 1 || queue.insert[0].IdempotencyKey != "access-42" {
		t.Fatalf("inserted keys = %+v, want [access-42]", queue.insert)
	}
}

func TestRedeliveredEventIsSkipped(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{}
	notifier := &fakeTaskStatus{}
	inbox := New(queue, notifier, transactorFunc(runTransaction))

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	for i := 0; i < 3; i++ {
		if err := inbox.PublishTaskStatus(ctx, event); err != nil {
			t.Fatalf("PublishTaskStatus() #%d = %v, want nil", i+1, err)
		}
	}

	if len(notifier.events) != 1 || len(notifier.published) != 1 {
		t.Fatalf("saved %d messages and published %d, want 1 each", len(notifier.events), len(notifier.published))
	}
	if key := queue.insert[0].IdempotencyKey; key != "task-42|approved" {
		t.Fatalf("idempotency key = %q, want task-42|approved", key)
	}
}

func TestFailedTransactionDoesNotPublish(t *testing.T) {
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name      string
		saveErr   error
		commitErr error
	}{
		{name: "save fails", saveErr: dbErr},
		{name: "commit fails", commitErr: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notifier := &fakeTaskStatus{saveErr: tc.saveErr}
			tx := transactorFunc(func(ctx context.Context, fn func(context.Context) error) error {
				err := fn(ctx)
				if !errors.Is(err, tc.saveErr) {
					t.Fatalf("transaction body error = %v, want %v", err, tc.saveErr)
				}
				if err != nil {
					return err
				}
				return tc.commitErr
			})
			inbox := New(&fakeQueue{}, notifier, tx)
			event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
			if err := inbox.PublishTaskStatus(t.Context(), event); !errors.Is(err, dbErr) {
				t.Fatalf("PublishTaskStatus() = %v, want %v", err, dbErr)
			}
			if len(notifier.published) != 0 {
				t.Fatal("published a message from a failed transaction")
			}
		})
	}
}

func TestQueueFailurePropagates(t *testing.T) {
	ctx := context.Background()
	dbErr := errors.New("database unavailable")
	queue := &fakeQueue{fail: dbErr}
	inbox := New(queue, &fakeTaskStatus{}, transactorFunc(runTransaction))

	event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
	if err := inbox.PublishTaskStatus(ctx, event); !errors.Is(err, dbErr) {
		t.Fatalf("PublishTaskStatus() = %v, want %v", err, dbErr)
	}
}
