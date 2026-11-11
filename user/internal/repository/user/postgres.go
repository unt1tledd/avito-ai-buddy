package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"avito-ai-buddy/user/internal/entity"
	sqlcUser "avito-ai-buddy/user/internal/repository/user/sqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type DB interface {
	sqlcUser.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

type PostgresRepository struct {
	db      DB
	queries sqlcUser.Querier
}

func NewPostgresRepository(db DB) *PostgresRepository {
	return &PostgresRepository{db: db, queries: sqlcUser.New(db)}
}

func (r *PostgresRepository) Create(ctx context.Context, profile entity.Profile, internship entity.Internship) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		queries := sqlcUser.New(tx)
		if err := ensureProfile(ctx, queries, profile); err != nil {
			return fmt.Errorf("ensure profile: %w", err)
		}
		if err := queries.EnsureTeamMembership(ctx, sqlcUser.EnsureTeamMembershipParams{
			TeamID: internship.TeamID,
			UserID: profile.UserID,
		}); err != nil {
			return fmt.Errorf("ensure team membership: %w", err)
		}

		var actualEndDate pgtype.Date
		if internship.ActualEndDate != nil {
			actualEndDate = pgtype.Date{Time: *internship.ActualEndDate, Valid: true}
		}
		var err error
		id, err = queries.CreateInternship(ctx, sqlcUser.CreateInternshipParams{
			InternUserID:   profile.UserID,
			TeamID:         internship.TeamID,
			MentorUserID:   internship.MentorUserID,
			Track:          internship.Track,
			StartDate:      pgtype.Date{Time: internship.StartDate, Valid: true},
			PlannedEndDate: pgtype.Date{Time: internship.PlannedEndDate, Valid: true},
			ActualEndDate:  actualEndDate,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ErrInternshipExists
		}
		return err
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			switch pgErr.ConstraintName {
			case "team_members_team_fk":
				return 0, entity.ErrTeamNotFound
			case "internships_mentor_membership_fk":
				return 0, entity.ErrMentorNotInTeam
			}
		}
		return 0, fmt.Errorf("create internship: %w", err)
	}
	return id, nil
}

func ensureProfile(ctx context.Context, queries sqlcUser.Querier, profile entity.Profile) error {
	inserted, err := queries.InsertProfile(ctx, sqlcUser.InsertProfileParams(profile))
	if err != nil || inserted != 0 {
		return err
	}
	existing, err := queries.GetProfile(ctx, profile.UserID)
	if err != nil {
		return err
	}
	middleNamesMatch := existing.MiddleName == nil && profile.MiddleName == nil ||
		existing.MiddleName != nil && profile.MiddleName != nil && *existing.MiddleName == *profile.MiddleName
	if existing.LastName != profile.LastName || existing.FirstName != profile.FirstName || !middleNamesMatch {
		return entity.ErrProfileConflict
	}
	return nil
}

func (r *PostgresRepository) GetMetadata(ctx context.Context, userID int64) (entity.Metadata, error) {
	row, err := r.queries.GetMetadata(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Metadata{}, entity.ErrMetadataNotFound
	}
	if err != nil {
		return entity.Metadata{}, fmt.Errorf("get internship metadata: %w", err)
	}

	var mentor *entity.Profile
	if row.MentorUserID != nil {
		mentor = &entity.Profile{
			UserID:     *row.MentorUserID,
			LastName:   *row.MentorLastName,
			FirstName:  *row.MentorFirstName,
			MiddleName: row.MentorMiddleName,
		}
	}
	var actualEndDate *time.Time
	var actualDuration *int32
	if row.ActualEndDate.Valid {
		actualEndDate = &row.ActualEndDate.Time
		actualDuration = &row.ActualDurationDays
	}
	return entity.Metadata{
		InternshipID: row.InternshipID,
		Intern: entity.Profile{
			UserID:     row.InternUserID,
			LastName:   row.LastName,
			FirstName:  row.FirstName,
			MiddleName: row.MiddleName,
		},
		Team:   entity.Team{ID: row.TeamID, Name: row.TeamName},
		Mentor: mentor,
		Internship: entity.Internship{
			TeamID:         row.TeamID,
			MentorUserID:   row.MentorUserID,
			Track:          row.Track,
			StartDate:      row.StartDate.Time,
			PlannedEndDate: row.PlannedEndDate.Time,
			ActualEndDate:  actualEndDate,
		},
		PlannedDurationDays: row.PlannedDurationDays,
		ActualDurationDays:  actualDuration,
	}, nil
}
