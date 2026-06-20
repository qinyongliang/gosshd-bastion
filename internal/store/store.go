package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	repo *Repository
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		path = filepath.Join(".", "gosshd.db")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	st := &Store{db: db}
	st.repo = &Repository{db: db}
	if err := st.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := st.ApplyMigrations(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func (s *Store) configure(ctx context.Context) error {
	return configureSQLite(ctx, s.db)
}

func configureSQLite(ctx context.Context, db *sql.DB) error {
	for _, stmt := range []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("configure sqlite %q: %w", stmt, err)
		}
	}
	return nil
}

func (s *Store) ApplyMigrations(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range migrations {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return fmt.Errorf("apply migration: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Repository() *Repository {
	return s.repo
}

func (s *Store) Close() error {
	return s.db.Close()
}
