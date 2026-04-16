package query

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Handler holds the Gin routes for the query service.
type Handler struct {
	store     *Store
	summarizer *Summarizer
	mu        sync.RWMutex
	jobs      map[string]*asyncJob
}

func NewHandler(store *Store, summarizer *Summarizer) *Handler {
	return &Handler{
		store:     store,
		summarizer: summarizer,
		jobs:      make(map[string]*asyncJob),
	}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	v1 := r.Group("/api/v1")
	v1.GET("/logs", h.getLogs)
	v1.GET("/logs/count", h.countLogs)
	v1.GET("/logs/summarize", h.summarizeLogs)
	v1.POST("/queries", h.createQuery)
	v1.GET("/queries/:query_id", h.getQuery)
}

// GET /api/v1/logs
func (h *Handler) getLogs(c *gin.Context) {
	p, err := parseParams(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.store.Query(c.Request.Context(), p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// GET /api/v1/logs/summarize
// Accepts the same filter params as GET /api/v1/logs.
// Fetches up to 500 matching lines and returns an LLM-generated summary.
func (h *Handler) summarizeLogs(c *gin.Context) {
	p, err := parseParams(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Cap at 500 for the LLM call.
	if p.Limit <= 0 || p.Limit > 500 {
		p.Limit = 500
	}

	result, err := h.store.Query(c.Request.Context(), p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	summary, err := h.summarizer.Summarize(c.Request.Context(), result.Logs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"log_count": len(result.Logs),
		"summary":   summary,
	})
}

// GET /api/v1/logs/count
func (h *Handler) countLogs(c *gin.Context) {
	p, err := parseParams(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	count, err := h.store.Count(c.Request.Context(), p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": count})
}

// ── Async queries ─────────────────────────────────────────────────────────────

type asyncJob struct {
	ID        string       `json:"query_id"`
	Status    string       `json:"status"` // pending | running | done | error
	CreatedAt time.Time    `json:"created_at"`
	Result    *QueryResult `json:"result,omitempty"`
	Error     string       `json:"error,omitempty"`
}

// POST /api/v1/queries
// Accepts the same query-string filters as GET /api/v1/logs.
// Returns immediately with a query_id; poll GET /api/v1/queries/:id for results.
func (h *Handler) createQuery(c *gin.Context) {
	p, err := parseParams(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	id := newQueryID()
	job := &asyncJob{ID: id, Status: "pending", CreatedAt: time.Now().UTC()}

	h.mu.Lock()
	h.jobs[id] = job
	h.mu.Unlock()

	// Run in background — use context.Background() so the query outlives the HTTP request.
	go func() {
		h.mu.Lock()
		job.Status = "running"
		h.mu.Unlock()

		result, err := h.store.Query(context.Background(), p)

		h.mu.Lock()
		defer h.mu.Unlock()
		if err != nil {
			job.Status = "error"
			job.Error = err.Error()
			return
		}
		job.Status = "done"
		job.Result = &result
	}()

	c.JSON(http.StatusAccepted, gin.H{"query_id": id, "status": "pending"})
}

// GET /api/v1/queries/:query_id
func (h *Handler) getQuery(c *gin.Context) {
	id := c.Param("query_id")

	h.mu.RLock()
	job, ok := h.jobs[id]
	h.mu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "query not found"})
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	c.JSON(http.StatusOK, job)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func parseParams(c *gin.Context) (Params, error) {
	var p Params

	if v := c.Query("start_time"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return p, fmt.Errorf("invalid start_time: use RFC3339 e.g. 2026-04-15T00:00:00Z")
		}
		p.StartTime = &t
	}
	if v := c.Query("end_time"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return p, fmt.Errorf("invalid end_time: use RFC3339 e.g. 2026-04-16T00:00:00Z")
		}
		p.EndTime = &t
	}

	p.Level = c.Query("level")
	p.MachineID = c.Query("machine_id")
	p.FilePath = c.Query("file_path")
	p.Keyword = c.Query("keyword")

	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return p, fmt.Errorf("invalid limit: must be a positive integer")
		}
		p.Limit = n
	}

	if token := c.Query("page_token"); token != "" {
		id, err := decodeToken(token)
		if err != nil {
			return p, fmt.Errorf("invalid page_token")
		}
		p.AfterID = id
	}

	return p, nil
}

func newQueryID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
