package migrations

import (
	"context"
	"embed"
	"io/fs"

	"avito-ai-buddy/pkg/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var files embed.FS

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := fs.Sub(files, "migrations")
	if err != nil {
		return err
	}
	return db.Migrate(ctx, pool, migrations, "chat_goose_db_version")
}
