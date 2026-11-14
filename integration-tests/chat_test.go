//go:build integration

package integration_test

import (
	chatpb "avito-ai-buddy/pkg/generated/chat/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"sync"
	"testing"
)

func TestChatHistoryIdempotencyAndIsolation(t *testing.T) {
	s := setup(t)
	_, token := s.intern(t)
	_, other := s.intern(t)
	require.Empty(t, s.history(t, token))
	first := s.send(t, token, "hello", 1)
	require.Equal(t, "answer:hello", first.Text)
	retry := s.send(t, token, "hello", 1)
	require.True(t, proto.Equal(first, retry))
	_, err := s.chat.SendMessage(rpcContext(t, token), &chatpb.SendMessageRequest{Text: "different", IdempotencyKey: 1})
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	s.send(t, token, "second", 2)
	all := s.history(t, token)
	require.Len(t, all, 4)
	require.Empty(t, s.history(t, other))
	// A key belongs to a conversation, not to the entire service.
	s.send(t, other, "other", 1)
	require.Len(t, s.history(t, other), 2)
	var paged []*chatpb.Message
	cursor := ""
	seen := map[string]bool{}
	for range 5 {
		page, err := s.chat.GetHistory(rpcContext(t, token), &chatpb.GetHistoryRequest{PageSize: 1, PageToken: cursor})
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Messages), 1)
		for _, m := range page.Messages {
			require.False(t, seen[m.Id], "duplicate across pages")
			seen[m.Id] = true
			// Pages move backwards; messages within each page are chronological.
			paged = append([]*chatpb.Message{m}, paged...)
		}
		cursor = page.NextPageToken
		if cursor == "" {
			break
		}
	}
	require.Empty(t, cursor, "pagination must terminate")
	require.Len(t, paged, len(all))
	for i := range all {
		require.True(t, proto.Equal(all[i], paged[i]))
	}
	r := s.call(t, "GET", s.chatURL+"/v1/chat/messages?page_size=100", token, nil, 200)
	var viaHTTP chatpb.GetHistoryResponse
	decode(t, r.body, &viaHTTP)
	require.True(t, proto.Equal(&chatpb.GetHistoryResponse{Messages: all}, &viaHTTP))
}

func TestChatRejectsUnauthorizedAndInvalidMessages(t *testing.T) {
	s := setup(t)
	for _, token := range []string{"", "broken-token"} {
		_, err := s.chat.GetHistory(rpcContext(t, token), &chatpb.GetHistoryRequest{})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
		s.call(t, "GET", s.chatURL+"/v1/chat/messages", token, nil, 401)
	}
	_, token := s.intern(t)
	for _, req := range []*chatpb.SendMessageRequest{{Text: " ", IdempotencyKey: 1}, {Text: "hello", IdempotencyKey: 0}} {
		_, err := s.chat.SendMessage(rpcContext(t, token), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	}
	require.Empty(t, s.history(t, token))
}

func TestConcurrentMessageRetriesSaveOnePair(t *testing.T) {
	s := setup(t)
	_, token := s.intern(t)
	const n = 6
	replies := make([]*chatpb.SendMessageResponse, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		ctx := rpcContext(t, token)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			replies[i], errs[i] = s.chat.SendMessage(ctx, &chatpb.SendMessageRequest{Text: "race", IdempotencyKey: 1})
		}()
	}
	close(start)
	wg.Wait()
	success := 0
	var id string
	for i, err := range errs {
		if err != nil {
			require.Equal(t, codes.Aborted, status.Code(err))
			continue
		}
		success++
		require.NotNil(t, replies[i].AssistantMessage)
		if id == "" {
			id = replies[i].AssistantMessage.Id
		}
		require.Equal(t, id, replies[i].AssistantMessage.Id)
	}
	require.Positive(t, success)
	require.Equal(t, id, s.send(t, token, "race", 1).Id)
	require.Len(t, s.history(t, token), 2)
}
