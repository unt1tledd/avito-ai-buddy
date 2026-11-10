package conversation

import (
	"errors"
	"testing"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/usecase/conversation/mocks"

	"go.uber.org/mock/gomock"
)

func TestGetOrCreate(t *testing.T) {
	ctx := t.Context()
	repo := mocks.NewMockrepository(gomock.NewController(t))
	service := NewService(repo)
	want := entity.Conversation{ID: 21, UserID: 7}
	stored := want
	repo.EXPECT().GetOrCreate(ctx, int64(7)).Return(&stored, nil)

	got, err := service.GetOrCreate(ctx, 7)
	if err != nil || got == nil || *got != want {
		t.Fatalf("GetOrCreate() = %+v, %v, want %+v, nil", got, err, want)
	}

	dbErr := errors.New("database unavailable")
	repo.EXPECT().GetOrCreate(ctx, int64(8)).Return(nil, dbErr)
	got, err = service.GetOrCreate(ctx, 8)
	if got != nil || !errors.Is(err, dbErr) {
		t.Fatalf("GetOrCreate() = %+v, %v, want nil, %v", got, err, dbErr)
	}
}
