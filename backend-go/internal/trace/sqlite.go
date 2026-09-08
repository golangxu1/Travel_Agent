package trace

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// migrations is embedded so every deployed binary carries the schema it
// needs. schema_migrations makes applying a newer migration idempotent.
//
//go:embed migrations/*.sql
var migrations embed.FS

var ErrNotFound = errors.New("trace not found")

type SQLiteRepository struct{ db *sql.DB }

func OpenSQLite(path string) (*SQLiteRepository, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("trace database path is empty")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create trace database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open trace database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	repo := &SQLiteRepository{db: db}
	if err := repo.configure(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repo.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

func (r *SQLiteRepository) Close() error { return r.db.Close() }

func (r *SQLiteRepository) configure() error {
	for _, pragma := range []string{"PRAGMA foreign_keys = ON", "PRAGMA journal_mode = WAL", "PRAGMA busy_timeout = 5000"} {
		if _, err := r.db.Exec(pragma); err != nil {
			return fmt.Errorf("configure sqlite: %w", err)
		}
	}
	return nil
}

func (r *SQLiteRepository) migrate() error {
	if _, err := r.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	entries, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	for _, name := range entries {
		version := filepath.Base(name)
		var exists int
		if err := r.db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		script, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := r.db.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(string(script)); err == nil {
			_, err = tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, datetime('now'))`, version)
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLiteRepository) Create(ctx context.Context, origin, destination string) (string, error) {
	id := newID()
	_, err := r.db.ExecContext(ctx, `INSERT INTO traces(id, origin, destination, created_at) VALUES (?, ?, ?, datetime('now'))`, id, origin, destination)
	return id, err
}

func newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err == nil {
		return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
	}
	return fmt.Sprintf("trace-%d-%d", os.Getpid(), time.Now().UnixNano())
}

func (r *SQLiteRepository) AddSpan(ctx context.Context, traceID string, span Span) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO trace_spans(trace_id, agent_name, agent_label, phase, start_offset_ms, duration_ms, input_tokens, output_tokens, tool_calls_count, output_chars, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, traceID, span.AgentName, span.AgentLabel, span.Phase, span.StartOffsetMS, span.DurationMS, span.InputTokens, span.OutputTokens, span.ToolCallsCount, span.OutputChars, span.Status)
	return err
}

func (r *SQLiteRepository) Finish(ctx context.Context, id string, durationMS int64, status, failure string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE traces SET total_duration_ms = ?, status = ?, error = ? WHERE id = ?`, durationMS, status, nullString(failure), id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (r *SQLiteRepository) List(ctx context.Context, limit int) ([]Trace, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, origin, destination, created_at, total_duration_ms, status, COALESCE(error, '') FROM traces ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	traces := make([]Trace, 0, limit)
	for rows.Next() {
		var item Trace
		if err := rows.Scan(&item.ID, &item.Origin, &item.Destination, &item.CreatedAt, &item.TotalDurationMS, &item.Status, &item.Error); err != nil {
			return nil, err
		}
		traces = append(traces, item)
	}
	return traces, rows.Err()
}

func (r *SQLiteRepository) Get(ctx context.Context, id string) (*Detail, error) {
	var detail Detail
	err := r.db.QueryRowContext(ctx, `SELECT id, origin, destination, created_at, total_duration_ms, status, COALESCE(error, '') FROM traces WHERE id = ?`, id).Scan(&detail.Trace.ID, &detail.Trace.Origin, &detail.Trace.Destination, &detail.Trace.CreatedAt, &detail.Trace.TotalDurationMS, &detail.Trace.Status, &detail.Trace.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT agent_name, agent_label, phase, start_offset_ms, duration_ms, input_tokens, output_tokens, tool_calls_count, output_chars, status FROM trace_spans WHERE trace_id = ? ORDER BY phase, start_offset_ms, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	detail.Spans = make([]Span, 0)
	for rows.Next() {
		var span Span
		if err := rows.Scan(&span.AgentName, &span.AgentLabel, &span.Phase, &span.StartOffsetMS, &span.DurationMS, &span.InputTokens, &span.OutputTokens, &span.ToolCallsCount, &span.OutputChars, &span.Status); err != nil {
			return nil, err
		}
		detail.Spans = append(detail.Spans, span)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &detail, nil
}
