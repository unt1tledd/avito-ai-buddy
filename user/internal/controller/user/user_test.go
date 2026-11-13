package user_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	userv1 "avito-ai-buddy/pkg/generated/user/v1"
	"avito-ai-buddy/user/internal/controller/user"
	"avito-ai-buddy/user/internal/entity"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type serviceStub struct {
	create      func(context.Context, entity.Profile, entity.Internship) (int64, error)
	getMetadata func(context.Context, int64) (entity.Metadata, error)
}

func (s serviceStub) Create(ctx context.Context, profile entity.Profile, internship entity.Internship) (int64, error) {
	return s.create(ctx, profile, internship)
}

func (s serviceStub) GetMetadata(ctx context.Context, id int64) (entity.Metadata, error) {
	return s.getMetadata(ctx, id)
}

func createRequest() *userv1.CreateRequest {
	return &userv1.CreateRequest{
		UserId: 42, FirstName: "Ivan", LastName: "Ivanov", TeamId: 7, Track: "Backend",
		StartDate: "2026-06-01", PlannedEndDate: "2026-07-01",
	}
}

func TestCreate(t *testing.T) {
	for _, withOptionalFields := range []bool{false, true} {
		t.Run(fmt.Sprintf("optional fields=%t", withOptionalFields), func(t *testing.T) {
			ctx := t.Context()
			req := createRequest()
			wantProfile := entity.Profile{UserID: 42, FirstName: "Ivan", LastName: "Ivanov"}
			wantInternship := entity.Internship{
				TeamID: 7, Track: "Backend",
				StartDate:      time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
				PlannedEndDate: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			}
			if withOptionalFields {
				req.MiddleName = new("Sergeevich")
				req.MentorUserId = new(int64(11))
				req.ActualEndDate = new("2026-06-25")
				wantProfile.MiddleName = new("Sergeevich")
				wantInternship.MentorUserID = new(int64(11))
				wantInternship.ActualEndDate = new(time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC))
			}
			service := serviceStub{create: func(gotCtx context.Context, profile entity.Profile, internship entity.Internship) (int64, error) {
				if gotCtx != ctx || !reflect.DeepEqual(profile, wantProfile) || !reflect.DeepEqual(internship, wantInternship) {
					t.Fatalf("Create received profile %+v and internship %+v, want %+v and %+v", profile, internship, wantProfile, wantInternship)
				}
				return 101, nil
			}}
			got, err := user.NewUserServer(service, zap.NewNop()).Create(ctx, req)
			if err != nil || got.GetInternshipId() != 101 {
				t.Fatalf("Create = (%v, %v), want internship ID 101", got, err)
			}
		})
	}
}

func TestCreateRejectsInvalidRequest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*userv1.CreateRequest)
	}{
		{"invalid user ID", func(req *userv1.CreateRequest) { req.UserId = 0 }},
		{"invalid start date", func(req *userv1.CreateRequest) { req.StartDate = "2026-02-29" }},
		{"invalid planned end date", func(req *userv1.CreateRequest) { req.PlannedEndDate = "0000-07-01" }},
		{"invalid actual end date", func(req *userv1.CreateRequest) { req.ActualEndDate = new("2026-07-01T00:00:00Z") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := createRequest()
			tc.change(req)
			got, err := user.NewUserServer(nil, zap.NewNop()).Create(t.Context(), req)
			if got != nil || status.Code(err) != codes.InvalidArgument {
				t.Fatalf("Create = (%v, %v), want InvalidArgument", got, err)
			}
		})
	}
}

func TestCreateMapsServiceErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code codes.Code
	}{
		{entity.ErrInvalidInput, codes.InvalidArgument},
		{entity.ErrMentorNotInTeam, codes.InvalidArgument},
		{entity.ErrTeamNotFound, codes.NotFound},
		{entity.ErrProfileConflict, codes.AlreadyExists},
		{entity.ErrInternshipExists, codes.AlreadyExists},
		{context.Canceled, codes.Canceled},
		{context.DeadlineExceeded, codes.DeadlineExceeded},
		{errors.New("database unavailable"), codes.Internal},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			service := serviceStub{create: func(context.Context, entity.Profile, entity.Internship) (int64, error) {
				return 0, fmt.Errorf("create internship: %w", tc.err)
			}}
			got, err := user.NewUserServer(service, zap.NewNop()).Create(t.Context(), createRequest())
			assertRPCError(t, got == nil, err, tc.code)
		})
	}
}

func TestGetMetadata(t *testing.T) {
	metadata := entity.Metadata{
		InternshipID: 101,
		Intern:       entity.Profile{UserID: 42, FirstName: "Ivan", LastName: "Ivanov", MiddleName: new("Sergeevich")},
		Team:         entity.Team{ID: 7, Name: "Platform"},
		Internship: entity.Internship{
			Track:          "Backend",
			StartDate:      time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
			PlannedEndDate: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		},
		PlannedDurationDays: 30,
	}
	want := &userv1.GetMetadataResponse{
		InternshipId:        101,
		Intern:              &userv1.GetMetadataResponse_Profile{UserId: 42, FirstName: "Ivan", LastName: "Ivanov", MiddleName: new("Sergeevich")},
		Team:                &userv1.GetMetadataResponse_Team{TeamId: 7, Name: "Platform"},
		Track:               "Backend",
		StartDate:           "2026-06-01",
		PlannedEndDate:      "2026-07-01",
		PlannedDurationDays: 30,
	}
	for _, completed := range []bool{false, true} {
		t.Run(fmt.Sprintf("completed=%t", completed), func(t *testing.T) {
			ctx := t.Context()
			result := metadata
			response := proto.CloneOf(want)
			if completed {
				result.Mentor = &entity.Profile{UserID: 11, FirstName: "Anna", LastName: "Petrova"}
				result.Internship.ActualEndDate = &result.Internship.StartDate
				result.ActualDurationDays = new(int32(0))
				response.Mentor = &userv1.GetMetadataResponse_Profile{UserId: 11, FirstName: "Anna", LastName: "Petrova"}
				response.ActualEndDate = new("2026-06-01")
				response.ActualDurationDays = new(int32(0))
			}
			service := serviceStub{getMetadata: func(gotCtx context.Context, id int64) (entity.Metadata, error) {
				if gotCtx != ctx || id != 42 {
					t.Fatalf("unexpected service call with user ID %d", id)
				}
				return result, nil
			}}
			got, err := user.NewUserServer(service, zap.NewNop()).GetMetadata(ctx, &userv1.GetMetadataRequest{UserId: 42})
			if err != nil || !proto.Equal(got, response) {
				t.Fatalf("GetMetadata = (%v, %v), want (%v, nil)", got, err, response)
			}
		})
	}
}

func TestGetMetadataMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   int64
		err  error
		code codes.Code
	}{
		{name: "invalid request", code: codes.InvalidArgument},
		{name: "invalid input", id: 42, err: entity.ErrInvalidInput, code: codes.InvalidArgument},
		{name: "missing metadata", id: 42, err: entity.ErrMetadataNotFound, code: codes.NotFound},
		{name: "canceled", id: 42, err: context.Canceled, code: codes.Canceled},
		{name: "deadline exceeded", id: 42, err: context.DeadlineExceeded, code: codes.DeadlineExceeded},
		{name: "unexpected error", id: 42, err: errors.New("database unavailable"), code: codes.Internal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := serviceStub{getMetadata: func(context.Context, int64) (entity.Metadata, error) {
				if tc.err == nil {
					t.Fatal("service called with an invalid user ID")
				}
				return entity.Metadata{}, fmt.Errorf("get metadata: %w", tc.err)
			}}
			got, err := user.NewUserServer(service, zap.NewNop()).GetMetadata(t.Context(), &userv1.GetMetadataRequest{UserId: tc.id})
			assertRPCError(t, got == nil, err, tc.code)
		})
	}
}

func assertRPCError(t *testing.T, emptyResponse bool, err error, want codes.Code) {
	t.Helper()
	if !emptyResponse || status.Code(err) != want {
		t.Fatalf("empty response = %t, error = %v, want empty response and %v", emptyResponse, err, want)
	}
	if want == codes.Internal && status.Convert(err).Message() != "internal server error" {
		t.Errorf("unexpected client-facing error: %v", err)
	}
}
