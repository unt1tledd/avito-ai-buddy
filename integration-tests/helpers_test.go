//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	chatpb "avito-ai-buddy/pkg/generated/chat/v1"
	taskpb "avito-ai-buddy/pkg/generated/task/v1"
	userpb "avito-ai-buddy/pkg/generated/user/v1"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const password = "Integration-password-123"
const teamID int64 = 900001
const trackerToken = "integration-tracker-token"

type suite struct {
	auth, userURL, chatURL, taskURL string
	http                            *http.Client
	users                           userpb.UserServiceClient
	chat                            chatpb.ChatServiceClient
	actions                         chatpb.ActionServiceClient
	tasks                           taskpb.TaskServiceClient
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func setup(t *testing.T) *suite {
	t.Helper()
	s := &suite{
		auth:    env("TEST_AUTH_URL", "http://auth:8080"),
		userURL: env("TEST_USER_URL", "http://user:8080"),
		chatURL: env("TEST_CHAT_URL", "http://chat:8080"),
		taskURL: env("TEST_TASK_URL", "http://task:8084"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
	for _, base := range []string{s.auth, s.userURL, s.chatURL, s.taskURL} {
		require.Eventually(t, func() bool {
			req, err := http.NewRequestWithContext(t.Context(), "GET", base+"/health", nil)
			if err != nil {
				return false
			}
			resp, err := s.http.Do(req)
			if err != nil {
				return false
			}
			defer func() { _ = resp.Body.Close() }()
			return resp.StatusCode == 200
		}, 60*time.Second, 250*time.Millisecond, "service not ready: %s", base)
	}
	dial := func(key, fallback string) *grpc.ClientConn {
		conn, err := grpc.NewClient(env(key, fallback), grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, conn.Close()) })
		return conn
	}
	s.users = userpb.NewUserServiceClient(dial("TEST_USER_GRPC", "user:9090"))
	cc := dial("TEST_CHAT_GRPC", "chat:50051")
	s.chat = chatpb.NewChatServiceClient(cc)
	s.actions = chatpb.NewActionServiceClient(cc)
	s.tasks = taskpb.NewTaskServiceClient(dial("TEST_TASK_GRPC", "task:9094"))
	return s
}

func rpcContext(t *testing.T, token string) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	if token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	}
	return ctx
}

type response struct {
	status  int
	body    []byte
	cookies []*http.Cookie
}

// request returns errors instead of calling Fatal, so concurrent callers can report on the test goroutine.
func (s *suite) request(ctx context.Context, method, url, token string, body any, cookies ...*http.Cookie) (response, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return response{}, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := s.http.Do(req)
	if err != nil {
		return response{}, err
	}
	defer func() { _ = res.Body.Close() }()
	data, err = io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return response{res.StatusCode, data, res.Cookies()}, err
}
func (s *suite) call(t *testing.T, method, url, token string, body any, want int, cookies ...*http.Cookie) response {
	t.Helper()
	r, err := s.request(t.Context(), method, url, token, body, cookies...)
	require.NoError(t, err)
	// Avoid logging successful auth payloads containing credentials.
	if r.status != want {
		t.Fatalf("%s %s: status %d, want %d", method, url, r.status, want)
	}
	return r
}
func decode(t *testing.T, body []byte, target any) {
	t.Helper()
	if p, ok := target.(proto.Message); ok {
		require.NoError(t, protojson.Unmarshal(body, p))
		return
	}
	require.NoError(t, json.Unmarshal(body, target))
}
func credentials(email string) map[string]string {
	return map[string]string{"email": email, "password": password}
}
func (s *suite) account(t *testing.T) (int64, string, string) {
	t.Helper()
	email := "it-" + strings.ToLower(rand.Text()) + "@example.test"
	r := s.call(t, "POST", s.auth+"/v1/auth/register", "", credentials(email), 201)
	var registered struct {
		UserID int64 `json:"user_id"`
	}
	decode(t, r.body, &registered)
	require.Positive(t, registered.UserID)
	r = s.call(t, "POST", s.auth+"/v1/auth/login", "", credentials(email), 200)
	var login struct {
		Token string `json:"access_token"`
	}
	decode(t, r.body, &login)
	require.NotEmpty(t, login.Token)
	return registered.UserID, login.Token, email
}
func profile(id int64) *userpb.CreateRequest {
	mentor := int64(910001)
	return &userpb.CreateRequest{UserId: id, FirstName: "Тест", LastName: "Интеграционный", TeamId: teamID, MentorUserId: &mentor, Track: "Backend", StartDate: "2026-01-01", PlannedEndDate: "2026-04-01"}
}
func (s *suite) intern(t *testing.T) (int64, string) {
	t.Helper()
	id, token, _ := s.account(t)
	_, err := s.users.Create(rpcContext(t, token), profile(id))
	require.NoError(t, err)
	return id, token
}
func (s *suite) send(t *testing.T, token, text string, key int64) *chatpb.Message {
	t.Helper()
	r, err := s.chat.SendMessage(rpcContext(t, token), &chatpb.SendMessageRequest{Text: text, IdempotencyKey: key})
	require.NoError(t, err)
	require.NotNil(t, r.AssistantMessage)
	return r.AssistantMessage
}
func (s *suite) history(t *testing.T, token string) []*chatpb.Message {
	t.Helper()
	r, err := s.chat.GetHistory(rpcContext(t, token), &chatpb.GetHistoryRequest{PageSize: 100})
	require.NoError(t, err)
	return r.Messages
}
func (s *suite) pending(t *testing.T, proposal string) *taskpb.AccessRequest {
	t.Helper()
	var found *taskpb.AccessRequest
	require.Eventually(t, func() bool {
		r, err := s.tasks.ListPending(rpcContext(t, trackerToken), &taskpb.ListPendingRequest{TeamId: teamID})
		if err != nil {
			t.Logf("waiting for task: %v", err)
			return false
		}
		for _, item := range r.Tasks {
			if fmt.Sprint(item.ProposalId) == proposal {
				found = item
				return true
			}
		}
		return false
	}, 30*time.Second, 100*time.Millisecond, "proposal %s did not reach task service", proposal)
	return found
}
