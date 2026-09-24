package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"goto/internal/link"

	sqliteDriver "modernc.org/sqlite"
	sqliteLib "modernc.org/sqlite/lib"
)

type Store struct {
	db *sql.DB
}

func Open(dsn string) (*Store, error) {
	slog.Info("Opening SQLite database", "dsn", dsn)

	// Enable WAL mode and busy timeout via DSN query params or pragmas if not present
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	pragmas := "_pragma=journal_mode%28WAL%29&_pragma=busy_timeout%285000%29&_pragma=synchronous%28NORMAL%29&_pragma=foreign_keys%28ON%29"
	fullDSN := fmt.Sprintf("%s%s%s", dsn, separator, pragmas)

	db, err := sql.Open("sqlite", fullDSN)
	if err != nil {
		slog.Error("Failed to open SQLite database", "error", err)
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Ping database to establish connection and verify DSN
	if err := db.Ping(); err != nil {
		_ = db.Close()
		slog.Error("Failed to ping SQLite database", "error", err)
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	maxOpen := max(4, runtime.NumCPU())
	maxIdle := max(2, runtime.NumCPU())
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		slog.Error("Failed to initialize SQLite schema", "error", err)
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	slog.Info("SQLite database initialized successfully", "max_open_conns", maxOpen, "max_idle_conns", maxIdle)
	return s, nil
}

func (s *Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS links (
		slug TEXT PRIMARY KEY,
		target TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) Close() error {
	slog.Info("Closing SQLite database")
	return s.db.Close()
}

func (s *Store) Get(ctx context.Context, slug string) (*link.Link, error) {
	query := `SELECT slug, target, created_at, updated_at FROM links WHERE slug = ?`
	row := s.db.QueryRowContext(ctx, query, slug)

	var l link.Link
	var createdAtStr, updatedAtStr string
	if err := row.Scan(&l.Slug, &l.Target, &createdAtStr, &updatedAtStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, link.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get link: %w", err)
	}

	var err error
	l.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtStr)
	if err != nil {
		return nil, fmt.Errorf("corrupt created_at timestamp: %w", err)
	}
	l.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("corrupt updated_at timestamp: %w", err)
	}

	return &l, nil
}

func (s *Store) List(ctx context.Context) ([]*link.Link, error) {
	query := `SELECT slug, target, created_at, updated_at FROM links ORDER BY slug ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query links: %w", err)
	}
	defer rows.Close()

	var links []*link.Link
	for rows.Next() {
		var l link.Link
		var createdAtStr, updatedAtStr string
		if err := rows.Scan(&l.Slug, &l.Target, &createdAtStr, &updatedAtStr); err != nil {
			return nil, fmt.Errorf("failed to scan link row: %w", err)
		}
		l.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("corrupt created_at timestamp: %w", err)
		}
		l.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAtStr)
		if err != nil {
			return nil, fmt.Errorf("corrupt updated_at timestamp: %w", err)
		}
		links = append(links, &l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during rows iteration: %w", err)
	}

	return links, nil
}

func (s *Store) Create(ctx context.Context, l *link.Link) error {
	now := time.Now().UTC()
	createdAt := l.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := l.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}

	query := `INSERT INTO links (slug, target, created_at, updated_at) VALUES (?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, l.Slug, l.Target, createdAt.Format(time.RFC3339Nano), updatedAt.Format(time.RFC3339Nano))
	if err != nil {
		var sqliteErr *sqliteDriver.Error
		if errors.As(err, &sqliteErr) && (sqliteErr.Code() == sqliteLib.SQLITE_CONSTRAINT || sqliteErr.Code() == sqliteLib.SQLITE_CONSTRAINT_PRIMARYKEY || sqliteErr.Code() == sqliteLib.SQLITE_CONSTRAINT_UNIQUE) {
			return link.ErrConflict
		}
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") || strings.Contains(strings.ToLower(err.Error()), "constraint failed") {
			return link.ErrConflict
		}
		return fmt.Errorf("failed to create link: %w", err)
	}

	l.CreatedAt = createdAt
	l.UpdatedAt = updatedAt
	return nil
}

func (s *Store) Update(ctx context.Context, l *link.Link) error {
	now := time.Now().UTC()
	query := `UPDATE links SET target = ?, updated_at = ? WHERE slug = ?`
	res, err := s.db.ExecContext(ctx, query, l.Target, now.Format(time.RFC3339Nano), l.Slug)
	if err != nil {
		return fmt.Errorf("failed to update link: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect rows affected: %w", err)
	}
	if rows == 0 {
		return link.ErrNotFound
	}

	l.UpdatedAt = now
	return nil
}

func (s *Store) Delete(ctx context.Context, slug string) error {
	query := `DELETE FROM links WHERE slug = ?`
	res, err := s.db.ExecContext(ctx, query, slug)
	if err != nil {
		return fmt.Errorf("failed to delete link: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect rows affected: %w", err)
	}
	if rows == 0 {
		return link.ErrNotFound
	}

	return nil
}
