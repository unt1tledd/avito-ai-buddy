package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"avito-ai-buddy/task/internal/entity"
	taskerrors "avito-ai-buddy/task/internal/errors"
)

type commandTasks struct {
	create func(entity.CreateRequest) (entity.AccessRequest, error)
	get    func(int64) (entity.AccessRequest, error)
}

func (s commandTasks) Create(_ context.Context, request entity.CreateRequest) (entity.AccessRequest, error) {
	return s.create(request)
}

func (s commandTasks) GetByProposal(_ context.Context, id int64) (entity.AccessRequest, error) {
	return s.get(id)
}

type resultPublisher func(int64, []byte) error

func (p resultPublisher) PublishTaskStatus(_ context.Context, key int64, payload []byte) error {
	return p(key, payload)
}

const identifiedCommand = `{"user_id":"7","proposal_id":"42","resource_id":"wiki","access_level":"read","dispatch_id":"attempt-1"}`

func TestCreateHandlerReportsPersistedTask(t *testing.T) {
	unavailable := errors.New("Kafka unavailable")
	for _, publishErr := range []error{nil, unavailable} {
		var published bool
		tasks := commandTasks{create: func(request entity.CreateRequest) (entity.AccessRequest, error) {
			if request != (entity.CreateRequest{UserID: 7, ProposalID: 42, ResourceID: "wiki", AccessLevel: "read"}) {
				t.Fatalf("unexpected command: %+v", request)
			}
			return entity.AccessRequest{ID: 51, UserID: 7, ProposalID: 42, Status: entity.Pending}, nil
		}}
		publisher := resultPublisher(func(key int64, data []byte) error {
			var event entity.StatusEvent
			if err := json.Unmarshal(data, &event); err != nil {
				t.Fatal(err)
			}
			if key != 51 || event != (entity.StatusEvent{UserID: 7, ProposalID: 42, TaskID: 51, Status: entity.Pending, DispatchID: "attempt-1"}) {
				t.Fatalf("unexpected result: key=%d, event=%+v", key, event)
			}
			published = true
			return publishErr
		})
		if err := NewCreateHandler(tasks, publisher).Handle(t.Context(), []byte(identifiedCommand)); !errors.Is(err, publishErr) || !published {
			t.Fatalf("result published=%t, err=%v", published, err)
		}
	}
}

func TestFailureReportReconcilesAmbiguousCreation(t *testing.T) {
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name string
		task entity.AccessRequest
		err  error
		want entity.Status
	}{
		{name: "no task", err: taskerrors.ErrNotFound, want: "failed"},
		{name: "created but acknowledgement lost", task: entity.AccessRequest{ID: 51, UserID: 7, Status: entity.Pending}, want: entity.Pending},
		{name: "already approved", task: entity.AccessRequest{ID: 51, UserID: 7, Status: entity.Approved}, want: entity.Approved},
		{name: "another owner", task: entity.AccessRequest{ID: 51, UserID: 8, Status: entity.Pending}},
		{name: "cannot establish outcome", err: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.task.ProposalID = 42
			var calls int
			tasks := commandTasks{get: func(id int64) (entity.AccessRequest, error) {
				if id != 42 {
					t.Fatalf("lookup %d", id)
				}
				return tc.task, tc.err
			}}
			publisher := resultPublisher(func(_ int64, data []byte) error {
				calls++
				var event entity.StatusEvent
				if err := json.Unmarshal(data, &event); err != nil {
					t.Fatal(err)
				}
				if event.Status != tc.want || event.UserID != 7 || event.ProposalID != 42 || event.DispatchID != "attempt-1" {
					t.Fatalf("result=%+v", event)
				}
				if tc.want == "failed" {
					var fields map[string]json.RawMessage
					if err := json.Unmarshal(data, &fields); err != nil {
						t.Fatal(err)
					}
					if _, exists := fields["task_id"]; exists {
						t.Fatalf("failure invented a task ID: %s", data)
					}
				}
				return nil
			})
			err := NewCreateHandler(tasks, publisher).ReportFailure(t.Context(), []byte(identifiedCommand))
			if tc.err == dbErr && !errors.Is(err, dbErr) {
				t.Fatalf("err=%v", err)
			}
			if tc.err != dbErr && err != nil {
				t.Fatal(err)
			}
			if (calls == 1) != (tc.want != "") {
				t.Fatalf("published %d events", calls)
			}
		})
	}
}

func TestMalformedCommandCannotChangeUserState(t *testing.T) {
	for _, data := range []string{`bad json`, `{}`, `{"user_id":"0","proposal_id":"42"}`} {
		if err := NewCreateHandler(commandTasks{}, nil).ReportFailure(t.Context(), []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
}
