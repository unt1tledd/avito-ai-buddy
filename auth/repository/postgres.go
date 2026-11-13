package auth

import (
	"context"
	"errors"

	"avito-ai-buddy/auth/entity"
	sqlcAuth "avito-ai-buddy/auth/repository/sqlc"

	"github.com/jackc/pgx/v5"
)

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

type (
	DB interface {
		Begin(ctx context.Context) (pgx.Tx, error)
		sqlcAuth.DBTX
	}
)

type postgresRepository struct {
	db      DB
	queries sqlcAuth.Querier
}

func NewPostgresRepository(db DB) *postgresRepository {
	return &postgresRepository{
		db:      db,
		queries: sqlcAuth.New(db),
	}
}

func (r *postgresRepository) CheckReady(ctx context.Context) error {
	_, err := r.db.Exec(ctx, "SELECT id, email, password_hash, created_at FROM auth.users LIMIT 0")
	return err
}

func (r *postgresRepository) GetUserByEmail(ctx context.Context, email string) (entity.User, error) {
	userDb, err := r.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.User{}, entity.ErrUserNotFound
		}
		return entity.User{}, err
	}

	return entity.User{
		ID:           userDb.ID,
		Email:        userDb.Email,
		PasswordHash: userDb.PasswordHash,
		CreatedAt:    userDb.CreatedAt.Time,
	}, nil
}

func (r *postgresRepository) CreateUser(ctx context.Context, email string, passwordHash string) (entity.User, error) {
	userDb, err := r.queries.CreateUser(ctx, sqlcAuth.CreateUserParams{
		Email:        email,
		PasswordHash: passwordHash,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.User{}, entity.ErrUserAlreadyExists
		}
		return entity.User{}, err
	}

	return entity.User{
		ID:           userDb.ID,
		Email:        userDb.Email,
		PasswordHash: userDb.PasswordHash,
		CreatedAt:    userDb.CreatedAt.Time,
	}, nil
}
