# ---- build stage ----
FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /index-service ./cmd/index-service

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12

COPY --from=builder /index-service /index-service

EXPOSE 50051

ENTRYPOINT ["/index-service"]
