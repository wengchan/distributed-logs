# Generic build for any of the cmd/* services.
#   docker build -f deploy/aws/Dockerfile --build-arg SERVICE=query-service -t <repo>:<tag> .
# Build context must be the repo root.

# ---- build stage ----
FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG SERVICE
RUN test -n "$SERVICE" || (echo "SERVICE build-arg is required" && exit 1)
RUN CGO_ENABLED=0 GOOS=linux go build -o /app ./cmd/${SERVICE}

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12

COPY --from=builder /app /app

ENTRYPOINT ["/app"]
