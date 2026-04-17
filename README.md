# Distributed Logs

A distributed log collection and query system built in Go. Log clients tail files on remote machines, push new lines to a central index service over gRPC, and a REST query service lets you search, count, and AI-summarize stored logs.

## Architecture

```
┌─────────────────┐        gRPC         ┌─────────────────┐
│   Log Client    │ ──────────────────▶ │  Index Service  │
│ (tails files)   │  PushLogs / GetOffset│  (gRPC :50051)  │
└─────────────────┘                     └────────┬────────┘
                                                 │
                                         INSERT / UPSERT
                                                 │
                                        ┌────────▼────────┐
                                        │    PostgreSQL    │
                                        │  offsets table  │
                                        │  logs table     │
                                        │  (partitioned)  │
                                        └────────▲────────┘
                                                 │
                                           SELECT queries
                                                 │
                                        ┌────────┴────────┐
                                        │  Query Service  │
                                        │  (HTTP :8080)   │
                                        └────────┬────────┘
                                                 │
                                         Claude Opus 4.6
                                        ┌────────▼────────┐
                                        │  Anthropic API  │
                                        │  (summarize)    │
                                        └─────────────────┘
```

## Components

| Component | Path | Description |
|-----------|------|-------------|
| **Index Service** | `cmd/index-service/` | gRPC server. Receives log lines from clients, parses them, stores in Postgres |
| **Log Client** | `cmd/log-client/` | Polls a directory for log files, tails new lines, pushes to index service |
| **Query Service** | `cmd/query-service/` | REST API for searching, counting, and AI-summarizing stored logs |

## Prerequisites

- Go 1.24+
- PostgreSQL 16
- Anthropic API key (for `/summarize` endpoint)

## Local Setup

### 1. Install and start Postgres

```bash
brew install postgresql@16
brew services start postgresql@16
# or: pg_ctl -D /opt/homebrew/var/postgresql@16 start
```

### 2. Create database and run migrations

```bash
make db-create
make db-migrate
```

### 3. Run the services (one tab each)

```bash
# Tab 1 — index service (gRPC)
make run-server

# Tab 2 — query service (HTTP)
export ANTHROPIC_API_KEY=sk-ant-api03-...
make run-query

# Tab 3 — log client (tails ./testlogs)
make run-client
```

## Makefile Reference

```bash
make pg-start      # Start local Postgres
make pg-stop       # Stop local Postgres
make pg-status     # Check if Postgres is accepting connections
make db-create     # Create the 'logs' database
make db-migrate    # Run SQL migrations
make db-reset      # Drop + recreate database and re-run migrations
make run-server    # Start index-service on :50051
make run-query     # Start query-service on :8080
make run-client    # Start log client (tails ./testlogs)
make build         # Compile all binaries to ./bin/
make clean         # Remove ./bin/
```

## REST API

Base URL: `http://localhost:8080`

### GET /api/v1/logs

Fetch logs with optional filters. Returns a page of results with cursor-based pagination.

**Query parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `start_time` | RFC3339 | Filter logs at or after this time |
| `end_time` | RFC3339 | Filter logs at or before this time |
| `level` | string | `DEBUG`, `INFO`, `WARNING`, `ERROR`, `FATAL` |
| `machine_id` | string | Filter by machine |
| `file_path` | string | Filter by source file |
| `keyword` | string | Full-text search in message |
| `limit` | int | Page size (default 100, max 1000) |
| `page_token` | string | Cursor from previous response for next page |

**Example:**
```bash
curl "localhost:8080/api/v1/logs?level=ERROR&limit=10"
curl "localhost:8080/api/v1/logs?start_time=2026-04-16T00:00:00Z&keyword=failed"
```

**Response:**
```json
{
  "logs": [
    {
      "id": 1,
      "machine_id": "machine-001",
      "file_path": "/logs/app.log",
      "start_time": "2026-04-16T10:00:00Z",
      "level": "ERROR",
      "message": "database connection failed"
    }
  ],
  "next_page_token": "dQ"
}
```

---

### GET /api/v1/logs/count

Count logs matching filters. Accepts the same query parameters as `/api/v1/logs`.

```bash
curl "localhost:8080/api/v1/logs/count?level=ERROR"
```

```json
{ "count": 42 }
```

---

### GET /api/v1/logs/summarize

Fetch up to 500 matching logs and return an AI-generated summary using Claude Opus 4.6. Accepts the same query parameters as `/api/v1/logs`.

Requires `ANTHROPIC_API_KEY` to be set.

```bash
curl "localhost:8080/api/v1/logs/summarize?level=ERROR"
curl "localhost:8080/api/v1/logs/summarize?start_time=2026-04-16T00:00:00Z"
```

```json
{
  "log_count": 12,
  "summary": "• System started normally at 10:00 UTC\n• 3 database connection errors between 10:15–10:18, all from machine-001\n• Recommended: check database connectivity on machine-001"
}
```

---

### POST /api/v1/queries

Submit an async query. Returns immediately with a `query_id`. Accepts the same query parameters as `/api/v1/logs`.

```bash
curl -X POST "localhost:8080/api/v1/queries?level=ERROR&limit=500"
```

```json
{ "query_id": "a1b2c3d4", "status": "pending" }
```

---

### GET /api/v1/queries/:query_id

Poll an async query for results.

```bash
curl "localhost:8080/api/v1/queries/a1b2c3d4"
```

```json
{
  "query_id": "a1b2c3d4",
  "status": "done",
  "created_at": "2026-04-16T10:00:00Z",
  "result": { "logs": [...], "next_page_token": "" }
}
```

Status values: `pending` → `running` → `done` | `error`

---

## gRPC API

Defined in `proto/indexservice/index_service.proto`.

| RPC | Request | Response | Description |
|-----|---------|----------|-------------|
| `GetOffset` | `machine_id`, `file_path` | `status`, `offset` | Returns the last saved byte offset for a file |
| `PushLogs` | `machine_id`, `file_path`, `start_offset`, `end_offset`, `log_lines[]` | `status` | Stores new log lines and advances the offset |

## Log Format

The log client parses structured lines automatically:

```
2026-04-16 10:00:00 INFO  Server is starting
2026-04-16 10:00:01 ERROR database connection failed
2026-04-16 10:00:02 DEBUG retrying in 5s
```

Supported levels: `DEBUG`, `INFO`, `WARNING`, `ERROR`, `FATAL`

Unstructured lines are stored with level `INFO` and the current timestamp.

## Database Schema

### offsets

Tracks how far each log client has read into each file.

| Column | Type | Description |
|--------|------|-------------|
| `machine_id` | TEXT | Unique machine identifier |
| `file_path` | TEXT | Absolute path to the log file |
| `offset` | BIGINT | Last read byte position |
| `updated_at` | TIMESTAMPTZ | Last update time |

Primary key: `(machine_id, file_path)`

### logs

Stores parsed log entries. Partitioned by `start_time` (monthly).

| Column | Type | Description |
|--------|------|-------------|
| `id` | BIGSERIAL | Auto-incrementing ID |
| `machine_id` | TEXT | Source machine |
| `file_path` | TEXT | Source file |
| `start_time` | TIMESTAMPTZ | Log entry timestamp |
| `level` | TEXT | Log level |
| `message` | TEXT | Log message body |

## Docker

```bash
# Build and run everything (requires Docker)
docker compose up --build
```

Starts: PostgreSQL primary + replica, index-service, log-client.

## Kubernetes

Manifests in `deploy/k8s/`:

```bash
kubectl apply -f deploy/k8s/statefulset.yaml   # Postgres StatefulSet
kubectl apply -f deploy/k8s/deployment.yaml    # index-service Deployment (2 replicas)
kubectl apply -f deploy/k8s/service.yaml       # ClusterIP Service on :50051
```

Requires a `postgres-secret` with `password` and `database_url` keys.

## Project Structure

```
├── cmd/
│   ├── index-service/    # gRPC server entrypoint
│   ├── log-client/       # Log tail + push client
│   └── query-service/    # HTTP query API entrypoint
├── internal/
│   ├── db/               # Postgres connection pool and queries
│   ├── logparse/         # Log line parser (level, timestamp, message)
│   ├── query/            # HTTP handlers, DB queries, LLM summarizer
│   └── server/           # gRPC handler implementations
├── migrations/           # SQL schema migrations
├── models/               # Shared Go structs (Offset, Log)
├── proto/indexservice/   # Protobuf definition + generated Go code
├── deploy/k8s/           # Kubernetes manifests
├── testlogs/             # Sample log files for local testing
├── Dockerfile            # index-service image
├── Dockerfile.client     # log-client image
└── docker-compose.yml    # Local dev stack
```
