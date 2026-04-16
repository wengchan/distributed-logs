DB_NAME     := logs
DB_USER     := $(shell whoami)
DB_URL      := postgres://$(DB_USER)@localhost:5432/$(DB_NAME)?sslmode=disable
PG_DATA     := /opt/homebrew/var/postgresql@16

.PHONY: help pg-start pg-stop pg-status db-create db-migrate db-reset \
        run-server run-client run-query build clean

help:
	@echo ""
	@echo "  pg-start    Start local Postgres"
	@echo "  pg-stop     Stop local Postgres"
	@echo "  pg-status   Check if Postgres is accepting connections"
	@echo "  db-create   Create the '$(DB_NAME)' database"
	@echo "  db-migrate  Run all migrations"
	@echo "  db-reset    Drop + recreate database and re-run migrations"
	@echo "  run-server  Start the index-service  (gRPC :50051)"
	@echo "  run-client  Tail ./testlogs and push to index-service"
	@echo "  run-query   Start the query-service  (HTTP :8080)"
	@echo "  build       Compile all binaries to ./bin/"
	@echo "  clean       Remove ./bin/"
	@echo ""

# ── Postgres ──────────────────────────────────────────────────────────────────

pg-start:
	pg_ctl -D $(PG_DATA) start

pg-stop:
	pg_ctl -D $(PG_DATA) stop

pg-status:
	pg_isready

# ── Database ──────────────────────────────────────────────────────────────────

db-create:
	createdb $(DB_NAME) || true

db-migrate:
	psql $(DB_NAME) -f migrations/001_create_offsets.sql
	psql $(DB_NAME) -f migrations/002_create_logs.sql

db-reset:
	dropdb --if-exists $(DB_NAME)
	createdb $(DB_NAME)
	$(MAKE) db-migrate

# ── Run ───────────────────────────────────────────────────────────────────────

run-server:
	DATABASE_URL="$(DB_URL)" go run ./cmd/index-service

run-client:
	go run ./cmd/log-client -path ./testlogs -interval 5s

run-query:
	DATABASE_URL="$(DB_URL)" ANTHROPIC_API_KEY="$(ANTHROPIC_API_KEY)" go run ./cmd/query-service

# ── Build ─────────────────────────────────────────────────────────────────────

build:
	mkdir -p bin
	go build -o bin/index-service ./cmd/index-service
	go build -o bin/log-client    ./cmd/log-client
	go build -o bin/query-service ./cmd/query-service

clean:
	rm -rf bin/
