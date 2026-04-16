package query

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store runs read-only queries against the logs table.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Params holds every filter the API accepts.
type Params struct {
	StartTime *time.Time
	EndTime   *time.Time
	Level     string
	MachineID string
	FilePath  string
	Keyword   string
	Limit     int
	AfterID   int64 // cursor decoded from page_token
}

// LogRow is one log entry returned to the caller.
type LogRow struct {
	ID        int64     `json:"id"`
	MachineID string    `json:"machine_id"`
	FilePath  string    `json:"file_path"`
	StartTime time.Time `json:"start_time"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// QueryResult holds the page of logs plus an optional continuation token.
type QueryResult struct {
	Logs          []LogRow `json:"logs"`
	NextPageToken string   `json:"next_page_token,omitempty"`
}

// Query fetches a filtered, paginated page of logs.
func (s *Store) Query(ctx context.Context, p Params) (QueryResult, error) {
	if p.Limit <= 0 || p.Limit > 1000 {
		p.Limit = 100
	}

	where, args := buildWhere(p)

	// Fetch one extra row to know whether a next page exists.
	q := fmt.Sprintf(`
		SELECT id, machine_id, file_path, start_time, level, message
		FROM logs
		%s
		ORDER BY id ASC
		LIMIT $%d`, where, len(args)+1)
	args = append(args, p.Limit+1)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return QueryResult{}, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()

	var logs []LogRow
	for rows.Next() {
		var r LogRow
		if err := rows.Scan(&r.ID, &r.MachineID, &r.FilePath, &r.StartTime, &r.Level, &r.Message); err != nil {
			return QueryResult{}, fmt.Errorf("scan row: %w", err)
		}
		logs = append(logs, r)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, fmt.Errorf("rows: %w", err)
	}

	var nextToken string
	if len(logs) > p.Limit {
		logs = logs[:p.Limit]
		nextToken = encodeToken(logs[len(logs)-1].ID)
	}

	return QueryResult{Logs: logs, NextPageToken: nextToken}, nil
}

// Count returns the total number of rows matching the filters (no pagination).
func (s *Store) Count(ctx context.Context, p Params) (int64, error) {
	where, args := buildWhere(p)
	q := fmt.Sprintf("SELECT COUNT(*) FROM logs %s", where)

	var n int64
	if err := s.pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count logs: %w", err)
	}
	return n, nil
}

// buildWhere constructs the WHERE clause and positional args from Params.
func buildWhere(p Params) (string, []any) {
	var conds []string
	var args []any
	i := 1

	add := func(cond string, val any) {
		conds = append(conds, fmt.Sprintf(cond, i))
		args = append(args, val)
		i++
	}

	if p.StartTime != nil {
		add("start_time >= $%d", *p.StartTime)
	}
	if p.EndTime != nil {
		add("start_time <= $%d", *p.EndTime)
	}
	if p.Level != "" {
		add("level = $%d", p.Level)
	}
	if p.MachineID != "" {
		add("machine_id = $%d", p.MachineID)
	}
	if p.FilePath != "" {
		add("file_path = $%d", p.FilePath)
	}
	if p.Keyword != "" {
		add("message ILIKE $%d", "%"+p.Keyword+"%")
	}
	if p.AfterID > 0 {
		add("id > $%d", p.AfterID)
	}

	if len(conds) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

func encodeToken(lastID int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(lastID, 10)))
}

func decodeToken(token string) (int64, error) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(b), 10, 64)
}
