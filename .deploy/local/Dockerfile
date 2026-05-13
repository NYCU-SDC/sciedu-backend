FROM golang:1.25.1 AS builder

WORKDIR /app

# Install golangci-lint, sqlc, and mockery
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.4.0
RUN go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.28.0
RUN go install github.com/vektra/mockery/v2@v2.53.5

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Run code generation (sqlc, mockery)
RUN ./scripts/create_sqlc_full_schema.sh
RUN sqlc generate
RUN go generate ./...

# Run lint check before building
RUN golangci-lint run ./...

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/backend cmd/backend/main.go

# Final stage
FROM golang:1.25.1

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/backend /app/backend

# Copy migrations
COPY internal/database/migrations /app/migrations

EXPOSE 8080

CMD ["/app/backend"]
