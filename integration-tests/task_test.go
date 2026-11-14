//go:build integration

package integration_test

import (
	chatpb "avito-ai-buddy/pkg/generated/chat/v1"
	taskpb "avito-ai-buddy/pkg/generated/task/v1"
	"fmt"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"strings"
	"sync"
	"testing"
	"time"
)

func (s *suite) accessTask(t *testing.T) (string, *taskpb.AccessRequest) {
	t.Helper()
	id, token := s.intern(t)
	_, other := s.intern(t)
	msg := s.send(t, token, "access", 1)
	require.NotNil(t, msg.Proposal)
	require.Equal(t, "integration-wiki", msg.Proposal.ResourceId)
	_, err := s.actions.ConfirmProposal(rpcContext(t, other), &chatpb.ConfirmProposalRequest{ProposalId: msg.Proposal.Id})
	require.Equal(t, codes.NotFound, status.Code(err), "another user must not confirm this proposal")
	for range 2 {
		_, err = s.actions.ConfirmProposal(rpcContext(t, token), &chatpb.ConfirmProposalRequest{ProposalId: msg.Proposal.Id})
		require.NoError(t, err)
	}
	task := s.pending(t, msg.Proposal.Id)
	require.Equal(t, id, task.UserId)
	require.Equal(t, teamID, task.TeamId)
	require.Equal(t, "integration-wiki", task.ResourceId)
	require.Equal(t, "read", task.AccessLevel)
	require.Equal(t, taskpb.AccessRequestStatus_ACCESS_REQUEST_STATUS_PENDING, task.Status)
	// Reconfirm after dispatch and read the persisted task through the HTTP gateway.
	_, err = s.actions.ConfirmProposal(rpcContext(t, token), &chatpb.ConfirmProposalRequest{ProposalId: msg.Proposal.Id})
	require.NoError(t, err)
	r := s.call(t, "GET", s.taskURL+"/internal/v1/tasks?team_id=900001", trackerToken, nil, 200)
	// Decode repeated protobuf messages using the wrapper's JSON representation.
	var list taskpb.ListPendingResponse
	decode(t, append(append([]byte(`{"tasks":`), r.body...), '}'), &list)
	count := 0
	for _, item := range list.Tasks {
		if item.ProposalId == task.ProposalId {
			count++
			require.True(t, proto.Equal(task, item))
		}
	}
	require.Equal(t, 1, count, "confirmation retries must produce one task")
	return token, task
}

func decision(task *taskpb.AccessRequest, state taskpb.AccessRequestStatus) *taskpb.DecideRequest {
	return &taskpb.DecideRequest{TaskId: task.TaskId, Decision: &taskpb.Decision{TeamId: teamID, DecidedByUserId: 910001, Status: state, Comment: "integration decision", ExternalRequestId: fmt.Sprintf("it-%d", task.TaskId)}}
}

func TestAccessRequestLifecycle(t *testing.T) {
	for _, state := range []taskpb.AccessRequestStatus{taskpb.AccessRequestStatus_ACCESS_REQUEST_STATUS_APPROVED, taskpb.AccessRequestStatus_ACCESS_REQUEST_STATUS_REJECTED} {
		t.Run(state.String(), func(t *testing.T) {
			s := setup(t)
			token, task := s.accessTask(t)
			wrong := decision(task, state)
			wrong.Decision.TeamId = 900002
			_, err := s.tasks.Decide(rpcContext(t, trackerToken), wrong)
			require.Equal(t, codes.NotFound, status.Code(err))
			req := decision(task, state)
			got, err := s.tasks.Decide(rpcContext(t, trackerToken), req)
			require.NoError(t, err)
			require.Equal(t, state, got.Status)
			require.NotNil(t, got.DecidedAt)
			require.Equal(t, req.Decision.ExternalRequestId, got.ExternalRequestId)
			again, err := s.tasks.Decide(rpcContext(t, trackerToken), req)
			require.NoError(t, err)
			require.True(t, proto.Equal(got, again))
			conflict := proto.Clone(req).(*taskpb.DecideRequest)
			conflict.Decision.Comment = "different"
			_, err = s.tasks.Decide(rpcContext(t, trackerToken), conflict)
			require.Equal(t, codes.AlreadyExists, status.Code(err))
			list, err := s.tasks.ListPending(rpcContext(t, trackerToken), &taskpb.ListPendingRequest{TeamId: teamID})
			require.NoError(t, err)
			for _, item := range list.Tasks {
				require.NotEqual(t, task.TaskId, item.TaskId)
			}
			word := "approved"
			if state == taskpb.AccessRequestStatus_ACCESS_REQUEST_STATUS_REJECTED {
				word = "rejected"
			}
			expected := fmt.Sprintf("Статус задачи %d изменён: %s.", task.TaskId, word)
			require.Eventually(t, func() bool {
				history, err := s.chat.GetHistory(rpcContext(t, token), &chatpb.GetHistoryRequest{PageSize: 100})
				if err != nil {
					return false
				}
				count := 0
				for _, m := range history.Messages {
					if m.Text == expected {
						count++
					}
				}
				return count == 1
			}, 30*time.Second, 100*time.Millisecond, "task event must appear once in chat history")
		})
	}
}

func TestConcurrentTaskDecisionsHaveOneWinner(t *testing.T) {
	s := setup(t)
	_, task := s.accessTask(t)
	states := []taskpb.AccessRequestStatus{taskpb.AccessRequestStatus_ACCESS_REQUEST_STATUS_APPROVED, taskpb.AccessRequestStatus_ACCESS_REQUEST_STATUS_REJECTED}
	errs := make([]error, 2)
	results := make([]*taskpb.AccessRequest, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, state := range states {
		ctx := rpcContext(t, trackerToken)
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results[i], errs[i] = s.tasks.Decide(ctx, decision(task, state)) }()
	}
	close(start)
	wg.Wait()
	wins := 0
	for i, err := range errs {
		if err != nil {
			require.Equal(t, codes.AlreadyExists, status.Code(err))
			continue
		}
		wins++
		require.Equal(t, states[i], results[i].Status)
		persisted, err := s.tasks.Decide(rpcContext(t, trackerToken), decision(task, states[i]))
		require.NoError(t, err)
		require.True(t, proto.Equal(results[i], persisted))
	}
	require.Equal(t, 1, wins)
}

func TestTaskAuthorizationAndValidation(t *testing.T) {
	s := setup(t)
	for _, token := range []string{"", "invalid"} {
		_, err := s.tasks.ListPending(rpcContext(t, token), &taskpb.ListPendingRequest{TeamId: teamID})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
		s.call(t, "GET", s.taskURL+"/internal/v1/tasks?team_id=900001", token, nil, 401)
	}
	_, err := s.tasks.ListPending(rpcContext(t, trackerToken), &taskpb.ListPendingRequest{TeamId: 0})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	r := s.call(t, "GET", s.taskURL+"/internal/v1/tasks?team_id=899999", trackerToken, nil, 200)
	require.Equal(t, "[]", strings.TrimSpace(string(r.body)))
}
