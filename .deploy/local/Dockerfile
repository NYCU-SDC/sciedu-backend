FROM golang:1.25.1 AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

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
