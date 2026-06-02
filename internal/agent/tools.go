package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"distributed-logs/internal/query"
)

// LogStore is the read-only slice of the query store the agent needs. Keeping
// it an interface (rather than *query.Store) makes the harness trivial to test
// with a fake.
type LogStore interface {
	Query(ctx context.Context, p query.Params) (query.QueryResult, error)
	Count(ctx context.Context, p query.Params) (int64, error)
}

// knownLevels is the set we report a breakdown over.
var knownLevels = []string{"ERROR", "WARN", "INFO", "DEBUG"}

// toolDefs is the catalogue advertised to the model. Descriptions are written
// for the model, not humans — they are the contract that makes the harness work.
var toolDefs = []anthropic.ToolUnionParam{
	{OfTool: &anthropic.ToolParam{
		Name:        "query_logs",
		Description: anthropic.String("Fetch raw log lines matching filters, ordered oldest-first. Use this to inspect the actual messages behind a spike or to look up context around an error. All filters are optional and combine with AND."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"level":      map[string]any{"type": "string", "description": "Exact level filter, e.g. ERROR, WARN, INFO, DEBUG."},
				"machine_id": map[string]any{"type": "string", "description": "Restrict to one machine."},
				"keyword":    map[string]any{"type": "string", "description": "Case-insensitive substring match on the message."},
				"after_id":   map[string]any{"type": "integer", "description": "Only return logs with id greater than this (cursor for the current window)."},
				"limit":      map[string]any{"type": "integer", "description": "Max rows to return (default 50, max 200)."},
			},
		},
	}},
	{OfTool: &anthropic.ToolParam{
		Name:        "count_logs",
		Description: anthropic.String("Count log lines matching filters without returning them. Cheap — use it to measure the size of a problem (e.g. how many ERRORs from a machine) before pulling samples."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"level":      map[string]any{"type": "string", "description": "Exact level filter, e.g. ERROR."},
				"machine_id": map[string]any{"type": "string", "description": "Restrict to one machine."},
				"keyword":    map[string]any{"type": "string", "description": "Case-insensitive substring match on the message."},
				"after_id":   map[string]any{"type": "integer", "description": "Only count logs with id greater than this."},
			},
		},
	}},
	{OfTool: &anthropic.ToolParam{
		Name:        "level_breakdown",
		Description: anthropic.String("Return the count of logs per severity level (ERROR/WARN/INFO/DEBUG). Use this first to get a situational overview before drilling in."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"machine_id": map[string]any{"type": "string", "description": "Restrict to one machine."},
				"after_id":   map[string]any{"type": "integer", "description": "Only consider logs with id greater than this (the current window)."},
			},
		},
	}},
}

// toolInput is the union of every field any tool accepts; unused fields stay zero.
type toolInput struct {
	Level     string `json:"level"`
	MachineID string `json:"machine_id"`
	Keyword   string `json:"keyword"`
	AfterID   int64  `json:"after_id"`
	Limit     int    `json:"limit"`
}

func (in toolInput) params() query.Params {
	return query.Params{
		Level:     in.Level,
		MachineID: in.MachineID,
		Keyword:   in.Keyword,
		AfterID:   in.AfterID,
		Limit:     in.Limit,
	}
}

// executor runs tool calls against the log store.
type executor struct {
	store LogStore
}

// execute dispatches one tool call and returns the result text plus whether it
// represents an error (which the harness reports back to the model as is_error).
func (e *executor) execute(ctx context.Context, name string, raw json.RawMessage) (string, bool) {
	var in toolInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return fmt.Sprintf("invalid tool input: %v", err), true
		}
	}

	switch name {
	case "query_logs":
		return e.queryLogs(ctx, in)
	case "count_logs":
		return e.countLogs(ctx, in)
	case "level_breakdown":
		return e.levelBreakdown(ctx, in)
	default:
		return fmt.Sprintf("unknown tool %q", name), true
	}
}

func (e *executor) queryLogs(ctx context.Context, in toolInput) (string, bool) {
	p := in.params()
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	res, err := e.store.Query(ctx, p)
	if err != nil {
		return err.Error(), true
	}
	if len(res.Logs) == 0 {
		return "No logs matched.", false
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d log line(s):\n", len(res.Logs))
	for _, l := range res.Logs {
		fmt.Fprintf(&sb, "#%d [%s] %s %s: %s\n",
			l.ID, l.StartTime.Format("2006-01-02 15:04:05"), l.Level, l.MachineID, l.Message)
	}
	if res.NextPageToken != "" {
		sb.WriteString("(more rows exist; raise limit or page with a larger after_id to see them)\n")
	}
	return sb.String(), false
}

func (e *executor) countLogs(ctx context.Context, in toolInput) (string, bool) {
	n, err := e.store.Count(ctx, in.params())
	if err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("%d matching log line(s).", n), false
}

func (e *executor) levelBreakdown(ctx context.Context, in toolInput) (string, bool) {
	var sb strings.Builder
	sb.WriteString("Level breakdown:\n")
	for _, lvl := range knownLevels {
		p := in.params()
		p.Level = lvl
		n, err := e.store.Count(ctx, p)
		if err != nil {
			return err.Error(), true
		}
		fmt.Fprintf(&sb, "  %-5s %d\n", lvl, n)
	}
	return sb.String(), false
}
