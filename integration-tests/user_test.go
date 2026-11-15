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
	id, token, _ := s.account(t)
	created, err := s.users.Create(rpcContext(t, token), profile(id))
	require.NoError(t, err)
	require.Positive(t, created.InternshipId)
	got, err := s.users.GetMetadata(rpcContext(t, token), &userpb.GetMetadataRequest{UserId: id})
	require.NoError(t, err)
	require.Equal(t, created.InternshipId, got.InternshipId)
	require.Equal(t, id, got.Intern.UserId)
	require.Equal(t, "Тест", got.Intern.FirstName)
	require.Equal(t, teamID, got.Team.TeamId)
	require.Equal(t, int64(910001), got.Mentor.UserId)
	require.Equal(t, int32(90), got.PlannedDurationDays)
	require.Equal(t, "2026-01-01", got.StartDate)
	require.Equal(t, "Backend", got.Track)
	r := s.call(t, "GET", fmt.Sprintf("%s/v1/user/%d/metadata", s.userURL, id), token, nil, 200)
	var gateway userpb.GetMetadataResponse
	decode(t, r.body, &gateway)
	require.True(t, proto.Equal(got, &gateway))
	_, err = s.users.Create(rpcContext(t, token), profile(id))
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
			id, token, _ := s.account(t)
			p := profile(id)
			tc.change(p)
			_, err := s.users.Create(rpcContext(t, token), p)
			require.Equal(t, tc.code, status.Code(err))
			_, err = s.users.GetMetadata(rpcContext(t, token), &userpb.GetMetadataRequest{UserId: id})
			require.Equal(t, codes.NotFound, status.Code(err))
			retry := profile(id)
			retry.FirstName = "Исправлен"
			_, err = s.users.Create(rpcContext(t, token), retry)
			require.NoError(t, err, "rollback must also remove the partially inserted profile")
		})
	}
}

func TestUserRejectsOtherUsers(t *testing.T) {
	s := setup(t)
	owner, token, _ := s.account(t)
	other, otherToken, _ := s.account(t)
	_, err := s.users.Create(rpcContext(t, ""), profile(owner))
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	_, err = s.users.Create(rpcContext(t, token), profile(other))
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = s.users.GetMetadata(rpcContext(t, otherToken), &userpb.GetMetadataRequest{UserId: owner})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	s.call(t, "POST", s.userURL+"/v1/user", "", profile(owner), 401)
	s.call(t, "POST", s.userURL+"/v1/user", token, profile(other), 403)
	s.call(t, "GET", fmt.Sprintf("%s/v1/user/%d/metadata", s.userURL, owner), "", nil, 401)
	s.call(t, "GET", fmt.Sprintf("%s/v1/user/%d/metadata", s.userURL, owner), otherToken, nil, 403)
	s.call(t, "POST", s.userURL+"/v1/user", token, profile(owner), 201)
	s.call(t, "GET", fmt.Sprintf("%s/v1/user/%d/metadata", s.userURL, owner), token, nil, 200)
}
