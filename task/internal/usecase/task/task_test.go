package task

import (
	"context"
	"errors"
	"slices"
	"testing"

	"avito-ai-buddy/task/internal/entity"
	taskerrors "avito-ai-buddy/task/internal/errors"
)

type repositoryStub struct {
	get    func(int64) (entity.AccessRequest, error)
	create func(entity.CreateRequest, int64) (entity.AccessRequest, error)
	list   func(int64, int64, int32) ([]entity.AccessRequest, error)
	decide func(entity.Decision) (entity.AccessRequest, error)
}

func (r repositoryStub) GetByProposal(_ context.Context, id int64) (entity.AccessRequest, error) {
	return r.get(id)
}

func (r repositoryStub) Create(_ context.Context, request entity.CreateRequest, teamID int64) (entity.AccessRequest, error) {
	return r.create(request, teamID)
}

func (r repositoryStub) ListPending(_ context.Context, teamID, afterID int64, limit int32) ([]entity.AccessRequest, error) {
	return r.list(teamID, afterID, limit)
}

func (r repositoryStub) Decide(_ context.Context, decision entity.Decision) (entity.AccessRequest, error) {
	return r.decide(decision)
}

type userStub func(int64) (int64, error)

func (f userStub) GetTeamID(_ context.Context, userID int64) (int64, error) {
	return f(userID)
}

func TestCreate(t *testing.T) {
	request := entity.CreateRequest{ProposalID: 17, UserID: 42, ResourceID: " analytics-db ", AccessLevel: " read "}
	existing := entity.AccessRequest{ID: 305, UserID: 42, TeamID: 7, ResourceID: "analytics-db", AccessLevel: "read", Status: entity.Approved}

	t.Run("stores the normalized request with the team from User", func(t *testing.T) {
		var calls []string
		repo := repositoryStub{
			get: func(id int64) (entity.AccessRequest, error) {
				calls = append(calls, "lookup")
				if id != 17 {
					t.Fatalf("lookup proposal %d, want 17", id)
				}
				return entity.AccessRequest{}, taskerrors.ErrNotFound
			},
			create: func(got entity.CreateRequest, teamID int64) (entity.AccessRequest, error) {
				calls = append(calls, "create")
				want := entity.CreateRequest{ProposalID: 17, UserID: 42, ResourceID: "analytics-db", AccessLevel: "read"}
				if got != want || teamID != 7 {
					t.Fatalf("create request=%+v team=%d, want %+v and 7", got, teamID, want)
				}
				return entity.AccessRequest{ID: 305, TeamID: teamID, Status: entity.Pending}, nil
			},
		}
		users := userStub(func(id int64) (int64, error) {
			calls = append(calls, "user")
			if id != 42 {
				t.Fatalf("lookup user %d, want 42", id)
			}
			return 7, nil
		})
		got, err := NewService(repo, users).Create(t.Context(), request)
		if err != nil || got.ID != 305 || got.Status != entity.Pending ||
			!slices.Equal(calls, []string{"lookup", "user", "create"}) {
			t.Fatalf("request=%+v, err=%v, calls=%v", got, err, calls)
		}
	})

	t.Run("duplicate returns the existing decision without contacting User", func(t *testing.T) {
		repo := repositoryStub{get: func(int64) (entity.AccessRequest, error) { return existing, nil }}
		got, err := NewService(repo, nil).Create(t.Context(), request)
		if err != nil || got != existing {
			t.Fatalf("request=%+v, err=%v, want %+v", got, err, existing)
		}
	})

	t.Run("same proposal with different access level is a conflict", func(t *testing.T) {
		repo := repositoryStub{get: func(int64) (entity.AccessRequest, error) { return existing, nil }}
		conflict := request
		conflict.AccessLevel = "write"
		if _, err := NewService(repo, nil).Create(t.Context(), conflict); !errors.Is(err, taskerrors.ErrProposalConflict) {
			t.Fatalf("err=%v, want proposal conflict", err)
		}
	})

	t.Run("User error prevents creation", func(t *testing.T) {
		repo := repositoryStub{get: func(int64) (entity.AccessRequest, error) {
			return entity.AccessRequest{}, taskerrors.ErrNotFound
		}}
		unavailable := errors.New("User unavailable")
		users := userStub(func(int64) (int64, error) { return 0, unavailable })
		if _, err := NewService(repo, users).Create(t.Context(), request); !errors.Is(err, unavailable) {
			t.Fatalf("err=%v, want %v", err, unavailable)
		}
	})

	t.Run("invalid input stops before the repository", func(t *testing.T) {
		invalid := request
		invalid.ResourceID = "  "
		if _, err := NewService(nil, nil).Create(t.Context(), invalid); !errors.Is(err, taskerrors.ErrInvalidInput) {
			t.Fatalf("err=%v, want invalid input", err)
		}
	})
}

func TestListPending(t *testing.T) {
	repo := repositoryStub{list: func(teamID, afterID int64, limit int32) ([]entity.AccessRequest, error) {
		if teamID != 7 || afterID != 305 || limit != 100 {
			t.Fatalf("unexpected pagination: %d, %d, %d", teamID, afterID, limit)
		}
		return []entity.AccessRequest{{ID: 306, Status: entity.Pending}}, nil
	}}
	requests, err := NewService(repo, nil).ListPending(t.Context(), 7, 305, 100)
	if err != nil || len(requests) != 1 || requests[0].ID != 306 {
		t.Fatalf("requests=%+v, err=%v", requests, err)
	}

	for _, limit := range []int32{0, 101} {
		if _, err := NewService(nil, nil).ListPending(t.Context(), 7, 0, limit); !errors.Is(err, taskerrors.ErrInvalidInput) {
			t.Fatalf("limit=%d, err=%v, want invalid input", limit, err)
		}
	}
}

func TestDecide(t *testing.T) {
	input := entity.Decision{TaskID: 305, TeamID: 7, DecidedByUserID: 51, Status: entity.Rejected, Comment: " reason ", ExternalRequestID: " ext-1 "}
	repo := repositoryStub{decide: func(got entity.Decision) (entity.AccessRequest, error) {
		want := entity.Decision{TaskID: 305, TeamID: 7, DecidedByUserID: 51, Status: entity.Rejected, Comment: "reason", ExternalRequestID: "ext-1"}
		if got != want {
			t.Fatalf("decision=%+v, want %+v", got, want)
		}
		return entity.AccessRequest{}, taskerrors.ErrAlreadyDecided
	}}
	if _, err := NewService(repo, nil).Decide(t.Context(), input); !errors.Is(err, taskerrors.ErrAlreadyDecided) {
		t.Fatalf("err=%v, want decision conflict", err)
	}

	input.Status = entity.Pending
	if _, err := NewService(nil, nil).Decide(t.Context(), input); !errors.Is(err, taskerrors.ErrInvalidInput) {
		t.Fatalf("err=%v, want invalid input", err)
	}
}
