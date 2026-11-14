//go:build integration

package integration_test

import (
	userpb "avito-ai-buddy/pkg/generated/user/v1"
	"fmt"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestUserPersistsAcrossGRPCAndHTTP(t *testing.T) {
	s := setup(t)
	id, _, _ := s.account(t)
	created, err := s.users.Create(rpcContext(t, ""), profile(id))
	require.NoError(t, err)
	require.Positive(t, created.InternshipId)
	got, err := s.users.GetMetadata(rpcContext(t, ""), &userpb.GetMetadataRequest{UserId: id})
	require.NoError(t, err)
	require.Equal(t, created.InternshipId, got.InternshipId)
	require.Equal(t, id, got.Intern.UserId)
	require.Equal(t, "Тест", got.Intern.FirstName)
	require.Equal(t, teamID, got.Team.TeamId)
	require.Equal(t, int64(910001), got.Mentor.UserId)
	require.Equal(t, int32(90), got.PlannedDurationDays)
	require.Equal(t, "2026-01-01", got.StartDate)
	require.Equal(t, "Backend", got.Track)
	r := s.call(t, "GET", fmt.Sprintf("%s/v1/user/%d/metadata", s.userURL, id), "", nil, 200)
	var gateway userpb.GetMetadataResponse
	decode(t, r.body, &gateway)
	require.True(t, proto.Equal(got, &gateway))
	_, err = s.users.Create(rpcContext(t, ""), profile(id))
	require.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestUserInvalidCreationDoesNotPersist(t *testing.T) {
	s := setup(t)
	for _, tc := range []struct {
		name   string
		change func(*userpb.CreateRequest)
		code   codes.Code
	}{
		{"unknown team", func(p *userpb.CreateRequest) { p.TeamId = 899999; p.MentorUserId = nil }, codes.NotFound},
		{"mentor from another team", func(p *userpb.CreateRequest) { v := int64(910002); p.MentorUserId = &v }, codes.InvalidArgument},
		{"invalid date", func(p *userpb.CreateRequest) { p.StartDate = "2026-02-30" }, codes.InvalidArgument},
		{"end before start", func(p *userpb.CreateRequest) { p.PlannedEndDate = "2025-01-01" }, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, _, _ := s.account(t)
			p := profile(id)
			tc.change(p)
			_, err := s.users.Create(rpcContext(t, ""), p)
			require.Equal(t, tc.code, status.Code(err))
			_, err = s.users.GetMetadata(rpcContext(t, ""), &userpb.GetMetadataRequest{UserId: id})
			require.Equal(t, codes.NotFound, status.Code(err))
			retry := profile(id)
			retry.FirstName = "Исправлен"
			_, err = s.users.Create(rpcContext(t, ""), retry)
			require.NoError(t, err, "rollback must also remove the partially inserted profile")
		})
	}
}
