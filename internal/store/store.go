// Package store is the SQLite persistence layer: sessions, push subscriptions,
// renewal requests and the notification log. Timestamps are Unix milliseconds.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var (
	// ErrNotFound is returned when a row does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrPendingExists is returned when a subscription already has a pending renewal.
	ErrPendingExists = errors.New("store: pending renewal already exists")
	// ErrNotPending is returned when resolving a renewal that is no longer pending.
	ErrNotPending = errors.New("store: renewal is not pending")
)

// Store wraps the database handle.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path with WAL and a busy timeout.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// DB exposes the handle for ad-hoc queries (tests, stats).
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Migrate applies embedded migrations that have not been applied yet and
// returns how many were applied.
func (s *Store) Migrate(ctx context.Context) (int, error) {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return 0, err
	}
	applied := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return 0, err
		}
		applied[v] = true
	}
	_ = rows.Close()

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return 0, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	n := 0
	for _, name := range names {
		if applied[name] {
			continue
		}
		sqlText, err := fs.ReadFile(migrationFS, path.Join("migrations", name))
		if err != nil {
			return n, err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return n, err
		}
		if _, err := tx.ExecContext(ctx, string(sqlText)); err != nil {
			_ = tx.Rollback()
			return n, fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, name, nowMs()); err != nil {
			_ = tx.Rollback()
			return n, err
		}
		if err := tx.Commit(); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func nowMs() int64 { return time.Now().UnixMilli() }

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var coded interface{ Code() int }
	if errors.As(err, &coded) && coded.Code() == 2067 { // SQLITE_CONSTRAINT_UNIQUE
		return true
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
