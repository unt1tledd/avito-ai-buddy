package user_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"avito-ai-buddy/user/internal/entity"
	"avito-ai-buddy/user/internal/usecase/user"
)

type repositoryStub struct {
	create      func(context.Context, entity.Profile, entity.Internship) (int64, error)
	getMetadata func(context.Context, int64) (entity.Metadata, error)
}

func (r repositoryStub) Create(ctx context.Context, profile entity.Profile, internship entity.Internship) (int64, error) {
	return r.create(ctx, profile, internship)
}

func (r repositoryStub) GetMetadata(ctx context.Context, id int64) (entity.Metadata, error) {
	return r.getMetadata(ctx, id)
}

func validInternship() (entity.Profile, entity.Internship) {
	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	return entity.Profile{UserID: 42, FirstName: "Ivan", LastName: "Ivanov"},
		entity.Internship{TeamID: 7, Track: "Backend", StartDate: start, PlannedEndDate: start.AddDate(0, 1, 0)}
}

func TestCreateNormalizesInput(t *testing.T) {
	for _, tc := range []struct {
		name       string
		middleName *string
		wantMiddle *string
	}{
		{name: "provided", middleName: new(" Sergeevich\t"), wantMiddle: new("Sergeevich")},
		{name: "blank", middleName: new(" \t")},
		{name: "omitted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			wantProfile, wantInternship := validInternship()
			wantProfile.MiddleName = tc.wantMiddle
			wantInternship.MentorUserID = new(int64(11))
			wantInternship.ActualEndDate = &wantInternship.StartDate
			wantInternship.PlannedEndDate = wantInternship.StartDate

			profile := wantProfile
			profile.FirstName = " Ivan "
			profile.LastName = "\tIvanov "
			profile.MiddleName = tc.middleName
			internship := wantInternship
			internship.Track = " Backend\t"

			repo := repositoryStub{create: func(gotCtx context.Context, gotProfile entity.Profile, gotInternship entity.Internship) (int64, error) {
				if gotCtx != ctx || !reflect.DeepEqual(gotProfile, wantProfile) || !reflect.DeepEqual(gotInternship, wantInternship) {
					t.Fatalf("Create received profile %+v and internship %+v, want %+v and %+v", gotProfile, gotInternship, wantProfile, wantInternship)
				}
				return 101, nil
			}}
			id, err := user.NewUserService(repo).Create(ctx, profile, internship)
			if err != nil || id != 101 {
				t.Fatalf("Create = (%d, %v), want (101, nil)", id, err)
			}
		})
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*entity.Profile, *entity.Internship)
	}{
		{"missing user ID", func(p *entity.Profile, _ *entity.Internship) { p.UserID = 0 }},
		{"negative team ID", func(_ *entity.Profile, i *entity.Internship) { i.TeamID = -1 }},
		{"blank first name", func(p *entity.Profile, _ *entity.Internship) { p.FirstName = " \t" }},
		{"blank last name", func(p *entity.Profile, _ *entity.Internship) { p.LastName = " \t" }},
		{"blank track", func(_ *entity.Profile, i *entity.Internship) { i.Track = " \t" }},
		{"invalid mentor ID", func(_ *entity.Profile, i *entity.Internship) { i.MentorUserID = new(int64(0)) }},
		{"intern as mentor", func(p *entity.Profile, i *entity.Internship) { i.MentorUserID = &p.UserID }},
		{"missing start date", func(_ *entity.Profile, i *entity.Internship) { i.StartDate = time.Time{} }},
		{"missing planned end date", func(_ *entity.Profile, i *entity.Internship) { i.PlannedEndDate = time.Time{} }},
		{"planned end before start", func(_ *entity.Profile, i *entity.Internship) { i.PlannedEndDate = i.StartDate.AddDate(0, 0, -1) }},
		{"actual end before start", func(_ *entity.Profile, i *entity.Internship) { i.ActualEndDate = new(i.StartDate.AddDate(0, 0, -1)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, internship := validInternship()
			tc.change(&profile, &internship)
			repo := repositoryStub{create: func(context.Context, entity.Profile, entity.Internship) (int64, error) {
				t.Fatal("repository called with invalid input")
				return 0, nil
			}}
			id, err := user.NewUserService(repo).Create(t.Context(), profile, internship)
			if id != 0 || !errors.Is(err, entity.ErrInvalidInput) {
				t.Fatalf("Create = (%d, %v), want (0, %v)", id, err, entity.ErrInvalidInput)
			}
		})
	}
}

func TestCreatePropagatesRepositoryError(t *testing.T) {
	profile, internship := validInternship()
	repo := repositoryStub{create: func(context.Context, entity.Profile, entity.Internship) (int64, error) {
		return 0, entity.ErrInternshipExists
	}}
	id, err := user.NewUserService(repo).Create(t.Context(), profile, internship)
	if id != 0 || !errors.Is(err, entity.ErrInternshipExists) {
		t.Fatalf("Create = (%d, %v), want (0, %v)", id, err, entity.ErrInternshipExists)
	}
}

func TestGetMetadata(t *testing.T) {
	profile, internship := validInternship()
	metadata := entity.Metadata{InternshipID: 101, Intern: profile, Internship: internship}
	for _, tc := range []struct {
		name   string
		id     int64
		result entity.Metadata
		err    error
	}{
		{name: "found", id: 42, result: metadata},
		{name: "missing", id: 42, err: entity.ErrMetadataNotFound},
		{name: "invalid ID", id: -1, err: entity.ErrInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			repo := repositoryStub{getMetadata: func(gotCtx context.Context, id int64) (entity.Metadata, error) {
				if gotCtx != ctx || id != 42 {
					t.Fatalf("unexpected repository call with user ID %d", id)
				}
				return tc.result, tc.err
			}}
			got, err := user.NewUserService(repo).GetMetadata(ctx, tc.id)
			if !errors.Is(err, tc.err) || !reflect.DeepEqual(got, tc.result) {
				t.Fatalf("GetMetadata = (%+v, %v), want (%+v, %v)", got, err, tc.result, tc.err)
			}
		})
	}
}
