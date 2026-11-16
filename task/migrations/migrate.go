package migrations

import (
	"context"
	"embed"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var embedded embed.FS

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sources, err := fs.Sub(embedded, "migrations")
	if err != nil {
		return err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, sources,
		goose.WithTableName("task_goose_db_version"), goose.WithSessionLocker(locker),
	)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}
