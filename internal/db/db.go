package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"distributed-logs/models"
)

// DB wraps a pgx connection pool.
type DB struct {
	pool *pgxpool.Pool
}

// New connects to Postgres and returns a DB.
// dsn example: "postgres://user:pass@localhost:5432/logs?sslmode=disable"
func New(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Close releases all pool connections.
func (d *DB) Close() {
	d.pool.Close()
}

// GetOffset returns the saved byte offset for a (machine, file) pair.
// Returns offset=0 and no error when the row does not exist yet.
func (d *DB) GetOffset(ctx context.Context, machineID, filePath string) (int64, error) {
	const q = `
		SELECT "offset" FROM offsets
		WHERE machine_id = $1 AND file_path = $2`

	var offset int64
	err := d.pool.QueryRow(ctx, q, machineID, filePath).Scan(&offset)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil // file not seen before — start from the beginning
	}
	if err != nil {
		return 0, fmt.Errorf("GetOffset: %w", err)
	}
	return offset, nil
}

// UpsertOffset inserts or updates the offset for a (machine, file) pair.
func (d *DB) UpsertOffset(ctx context.Context, machineID, filePath string, offset int64) error {
	const q = `
		INSERT INTO offsets (machine_id, file_path, "offset", updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (machine_id, file_path)
		DO UPDATE SET "offset" = EXCLUDED."offset", updated_at = EXCLUDED.updated_at`

	_, err := d.pool.Exec(ctx, q, machineID, filePath, offset, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("UpsertOffset: %w", err)
	}
	return nil
}

// InsertLogs bulk-inserts a slice of log entries.
func (d *DB) InsertLogs(ctx context.Context, logs []models.Log) error {
	if len(logs) == 0 {
		return nil
	}

	// pgx CopyFrom is the fastest bulk-insert path.
	rows := make([][]any, len(logs))
	for i, l := range logs {
		rows[i] = []any{l.MachineID, l.FilePath, l.StartTime, string(l.Level), l.Message}
	}

	_, err := d.pool.CopyFrom(
		ctx,
		[]string{"logs"},
		[]string{"machine_id", "file_path", "start_time", "level", "message"},
		newRowSource(rows),
	)
	if err != nil {
		return fmt.Errorf("InsertLogs: %w", err)
	}
	return nil
}

// rowSource adapts [][]any to pgx CopyFromSource.
type rowSource struct {
	rows [][]any
	idx  int
}

func newRowSource(rows [][]any) *rowSource { return &rowSource{rows: rows} }

func (r *rowSource) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *rowSource) Values() ([]any, error) { return r.rows[r.idx-1], nil }
func (r *rowSource) Err() error             { return nil }
