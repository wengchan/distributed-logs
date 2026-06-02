package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"distributed-logs/internal/query"
)

// Monitor drives the agent on a fixed interval: each tick it detects logs
// ingested since the last check, and if there are any, asks the agent to
// investigate that window. Reports are kept in a small ring buffer that the
// HTTP layer serves.
type Monitor struct {
	agent    *Agent
	store    *query.Store
	interval time.Duration

	mu      sync.RWMutex
	cursor  int64     // highest log id analyzed so far
	lastRun time.Time // wall-clock of the last tick
	reports []Report  // ring buffer, newest last
	maxKept int
}

// NewMonitor builds a monitor. bootstrap controls how many pre-existing log
// lines the very first tick looks back over, so a fresh deployment against an
// already-populated store still produces a report instead of waiting for new
// traffic.
func NewMonitor(a *Agent, store *query.Store, interval time.Duration) *Monitor {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Monitor{
		agent:    a,
		store:    store,
		interval: interval,
		maxKept:  50,
	}
}

// Start primes the cursor and runs the tick loop until ctx is cancelled.
// bootstrap is the number of recent existing logs to include in the first tick.
func (m *Monitor) Start(ctx context.Context, bootstrap int64) {
	maxID, err := m.store.MaxID(ctx)
	if err != nil {
		log.Printf("monitor: failed to read starting cursor: %v", err)
	}
	start := maxID - bootstrap
	if start < 0 {
		start = 0
	}
	m.mu.Lock()
	m.cursor = start
	m.mu.Unlock()
	log.Printf("monitor: starting at cursor=%d (max id=%d, bootstrap=%d), interval=%s",
		start, maxID, bootstrap, m.interval)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Run one immediately so we don't wait a full interval for the first report.
	m.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Printf("monitor: stopping")
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

// tick runs one monitoring cycle.
func (m *Monitor) tick(ctx context.Context) {
	m.mu.RLock()
	cursor := m.cursor
	m.mu.RUnlock()

	// Pull the new window so we know (a) whether anything arrived and (b) the
	// new high-water mark. The agent gets these lines as its initial observation
	// and can fetch more via tools.
	window, err := m.store.Query(ctx, query.Params{AfterID: cursor, Limit: 200})
	if err != nil {
		log.Printf("monitor: window query failed: %v", err)
		return
	}

	m.mu.Lock()
	m.lastRun = time.Now().UTC()
	m.mu.Unlock()

	if len(window.Logs) == 0 {
		return // nothing new — stay quiet
	}

	newMax := window.Logs[len(window.Logs)-1].ID
	task := buildTask(cursor, window)

	report, err := m.agent.Run(ctx, task)
	if err != nil {
		log.Printf("monitor: agent run failed: %v", err)
		return
	}

	m.record(report, newMax)
	log.Printf("monitor: analyzed %d new log(s) up to id=%d → severity=%s steps=%d tools=%d (%s)",
		len(window.Logs), newMax, report.Severity, report.Steps, len(report.ToolCalls), report.Duration)
}

// buildTask formats the window into the agent's opening prompt.
func buildTask(cursor int64, window query.QueryResult) string {
	var b []byte
	b = append(b, fmt.Sprintf("New log activity since id=%d: %d new line(s).\n", cursor, len(window.Logs))...)
	if window.NextPageToken != "" {
		b = append(b, "(window truncated at 200 lines — more exist; use tools with after_id to see the rest)\n"...)
	}
	b = append(b, "\nThe new window:\n"...)
	for _, l := range window.Logs {
		b = append(b, fmt.Sprintf("#%d [%s] %s %s: %s\n",
			l.ID, l.StartTime.Format("2006-01-02 15:04:05"), l.Level, l.MachineID, l.Message)...)
	}
	b = append(b, fmt.Sprintf("\nInvestigate this window (its logs have id > %d) and report.", cursor)...)
	return string(b)
}

// record stores a report and advances the cursor.
func (m *Monitor) record(r Report, newCursor int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cursor = newCursor
	m.reports = append(m.reports, r)
	if len(m.reports) > m.maxKept {
		m.reports = m.reports[len(m.reports)-m.maxKept:]
	}
}

// ── accessors for the HTTP layer ────────────────────────────────────────────

// Status is a lightweight health/overview snapshot.
type Status struct {
	LastRun     time.Time `json:"last_run"`
	Cursor      int64     `json:"cursor"`
	Interval    string    `json:"interval"`
	ReportCount int       `json:"report_count"`
	LatestSev   string    `json:"latest_severity,omitempty"`
}

func (m *Monitor) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := Status{
		LastRun:     m.lastRun,
		Cursor:      m.cursor,
		Interval:    m.interval.String(),
		ReportCount: len(m.reports),
	}
	if len(m.reports) > 0 {
		s.LatestSev = m.reports[len(m.reports)-1].Severity
	}
	return s
}

// Reports returns the kept reports, newest first.
func (m *Monitor) Reports() []Report {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Report, len(m.reports))
	for i, r := range m.reports {
		out[len(m.reports)-1-i] = r
	}
	return out
}

// Latest returns the most recent report, or false if none yet.
func (m *Monitor) Latest() (Report, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.reports) == 0 {
		return Report{}, false
	}
	return m.reports[len(m.reports)-1], true
}
