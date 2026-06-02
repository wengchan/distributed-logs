package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"distributed-logs/internal/query"
)

// fakeStore is an in-memory LogStore for testing the executor without a DB.
type fakeStore struct {
	rows       []query.LogRow
	lastParams query.Params
}

func (f *fakeStore) Query(_ context.Context, p query.Params) (query.QueryResult, error) {
	f.lastParams = p
	var out []query.LogRow
	for _, r := range f.rows {
		if p.Level != "" && r.Level != p.Level {
			continue
		}
		if p.AfterID > 0 && r.ID <= p.AfterID {
			continue
		}
		out = append(out, r)
	}
	return query.QueryResult{Logs: out}, nil
}

func (f *fakeStore) Count(_ context.Context, p query.Params) (int64, error) {
	res, _ := f.Query(context.Background(), p)
	return int64(len(res.Logs)), nil
}

func sampleStore() *fakeStore {
	t := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	return &fakeStore{rows: []query.LogRow{
		{ID: 1, MachineID: "m1", Level: "INFO", Message: "starting", StartTime: t},
		{ID: 2, MachineID: "m1", Level: "ERROR", Message: "db connection failed", StartTime: t},
		{ID: 3, MachineID: "m1", Level: "ERROR", Message: "db connection failed again", StartTime: t},
		{ID: 4, MachineID: "m1", Level: "WARN", Message: "retrying", StartTime: t},
	}}
}

func TestExecuteCountLogs(t *testing.T) {
	e := &executor{store: sampleStore()}
	out, isErr := e.execute(context.Background(), "count_logs", json.RawMessage(`{"level":"ERROR"}`))
	if isErr {
		t.Fatalf("unexpected error result: %s", out)
	}
	if !strings.Contains(out, "2 matching") {
		t.Errorf("want count of 2 ERRORs, got: %q", out)
	}
}

func TestExecuteQueryLogsAppliesAfterID(t *testing.T) {
	e := &executor{store: sampleStore()}
	out, isErr := e.execute(context.Background(), "query_logs", json.RawMessage(`{"after_id":2}`))
	if isErr {
		t.Fatalf("unexpected error result: %s", out)
	}
	// Only ids 3 and 4 are > 2.
	if strings.Contains(out, "#1 ") || strings.Contains(out, "#2 ") {
		t.Errorf("after_id=2 should exclude ids 1,2; got: %q", out)
	}
	if !strings.Contains(out, "#3 ") || !strings.Contains(out, "#4 ") {
		t.Errorf("after_id=2 should include ids 3,4; got: %q", out)
	}
}

func TestExecuteLevelBreakdown(t *testing.T) {
	e := &executor{store: sampleStore()}
	out, isErr := e.execute(context.Background(), "level_breakdown", json.RawMessage(`{}`))
	if isErr {
		t.Fatalf("unexpected error result: %s", out)
	}
	for _, want := range []string{"ERROR 2", "WARN  1", "INFO  1", "DEBUG 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("breakdown missing %q; got: %q", want, out)
		}
	}
}

func TestExecuteUnknownToolIsError(t *testing.T) {
	e := &executor{store: sampleStore()}
	out, isErr := e.execute(context.Background(), "nope", nil)
	if !isErr {
		t.Errorf("unknown tool should be an error, got: %q", out)
	}
}

func TestExecuteInvalidInputIsError(t *testing.T) {
	e := &executor{store: sampleStore()}
	_, isErr := e.execute(context.Background(), "count_logs", json.RawMessage(`{not json}`))
	if !isErr {
		t.Error("malformed JSON input should be an error")
	}
}
