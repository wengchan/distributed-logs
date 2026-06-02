// monitor-service runs an AI agent that watches the log store in real time.
//
// On a fixed interval the agent investigates newly-ingested logs using a
// tool-use harness (query_logs / count_logs / level_breakdown) and emits a
// severity-tagged report. Reports are served over HTTP, and an on-demand
// /analyze endpoint lets you ask the agent an ad-hoc question.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"distributed-logs/internal/agent"
	"distributed-logs/internal/query"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres@localhost:5432/logs?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("db ping failed: %v", err)
	}

	store := query.NewStore(pool)
	ai := agent.New(os.Getenv("ANTHROPIC_API_KEY"), store).
		WithModel(anthropic.Model(os.Getenv("MONITOR_MODEL")))

	interval := envDuration("MONITOR_INTERVAL", 30*time.Second)
	bootstrap := int64(envInt("MONITOR_BOOTSTRAP", 100))
	monitor := agent.NewMonitor(ai, store, interval)

	// Run the monitor loop until SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go monitor.Start(ctx, bootstrap)

	// ── HTTP API ──────────────────────────────────────────────────────────────
	r := gin.Default()
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, monitor.Status())
	})
	r.GET("/reports", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"reports": monitor.Reports()})
	})
	r.GET("/reports/latest", func(c *gin.Context) {
		rep, ok := monitor.Latest()
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "no reports yet"})
			return
		}
		c.JSON(http.StatusOK, rep)
	})
	// Ad-hoc investigation: POST {"question": "..."} and the agent answers using
	// its tools. Falls back to a generic triage if no question is given.
	r.POST("/analyze", func(c *gin.Context) {
		var body struct {
			Question string `json:"question"`
		}
		_ = c.ShouldBindJSON(&body)
		task := body.Question
		if task == "" {
			task = "Perform a full triage of the current log store and report its health."
		}
		rep, err := ai.Run(c.Request.Context(), task)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, rep)
	})

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8082"
	}

	srv := &http.Server{Addr: addr, Handler: r}
	go func() {
		log.Printf("monitor-service listening at %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		log.Printf("invalid %s=%q, using default %s", key, v, def)
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("invalid %s=%q, using default %d", key, v, def)
	}
	return def
}
