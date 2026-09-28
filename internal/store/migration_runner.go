package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql audit_migrations/*.sql
var schemaFiles embed.FS

func applySchemaMigrations(ctx context.Context, db *sql.DB, directory, legacyTable string, upgradeLegacy func(context.Context, *sql.Tx) error) error {
	paths, err := fs.Glob(schemaFiles, directory+"/*.sql")
	if err != nil {
		return fmt.Errorf("load %s migrations: %w", directory, err)
	}
	if len(paths) == 0 {
		return fmt.Errorf("no %s migrations found", directory)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var legacyExists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)", legacyTable).Scan(&legacyExists); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	var appliedCount int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&appliedCount); err != nil {
		return err
	}

	for _, file := range paths {
		name := path.Base(file)
		versionText, _, ok := strings.Cut(name, "_")
		if !ok {
			return fmt.Errorf("invalid migration filename %s", name)
		}
		version, err := strconv.Atoi(versionText)
		if err != nil {
			return fmt.Errorf("invalid migration version %s: %w", name, err)
		}
		var recordedName string
		err = tx.QueryRowContext(ctx, "SELECT name FROM schema_migrations WHERE version = ?", version).Scan(&recordedName)
		if err == nil {
			if recordedName != name {
				return fmt.Errorf("migration %d changed from %s to %s", version, recordedName, name)
			}
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		content, err := schemaFiles.ReadFile(file)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if version == 1 && appliedCount == 0 && legacyExists && upgradeLegacy != nil {
			if err := upgradeLegacy(ctx, tx); err != nil {
				return fmt.Errorf("upgrade legacy %s schema: %w", directory, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)", version, name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
