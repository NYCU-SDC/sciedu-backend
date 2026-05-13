# Local Development Environment

This directory contains the Docker Compose configuration for local development and testing the complete deployment.

## Quick Start

### Prerequisites
- Docker and Docker Compose installed
- Ports 5432 and 8080 available on your host machine

### Starting the Complete Stack

```bash
# Clean up previous test data to avoid pollution
docker compose -f .deploy/local/compose.yaml down -v

# Build and start all services (PostgreSQL + Backend)
docker compose -f .deploy/local/compose.yaml up -d --build
```

Or use from the `.deploy/local` directory:

```bash
cd .deploy/local

# Clean up
docker compose down -v

# Start services with build
docker compose up -d --build
```

### What Gets Deployed

1. **PostgreSQL 18.1**: Database server with health checks
2. **Backend Service**: Go application built from source
3. **Automatic Migration**: Database migrations run automatically on backend startup
4. **API Server**: Exposed on `http://localhost:8080`

## Service Details

### PostgreSQL Connection

- **Host**: `localhost`
- **Port**: `5432`
- **Database**: `sciedu`
- **User**: `postgres`
- **Password**: `password`
- **Database URL**: `postgres://postgres:password@localhost:5432/sciedu?sslmode=disable`

### Backend Service

- **API Endpoint**: `http://localhost:8080`
- **Health Check**: `http://localhost:8080/api/healthz`
- **Environment**: `local` (debug mode enabled)
- **Build**: Multi-stage Docker build from source code

## Architecture

```
┌─────────────────────────────────────┐
│     Backend Container               │
│  - Go application                   │
│  - Auto-runs migrations             │
│  - Listens on :8080                 │
│  - Depends on PostgreSQL healthy    │
└─────────────────┬───────────────────┘
                  │
                  │ sciedu-local network
                  │
┌─────────────────▼───────────────────┐
│     PostgreSQL Container            │
│  - PostgreSQL 18.1                  │
│  - Persistent volume                │
│  - Health checks enabled            │
└─────────────────────────────────────┘
```

## Checking Status

```bash
# View running containers
docker compose ps

# Check backend logs
docker logs sciedu-backend-local

# Check database logs
docker logs sciedu-local-postgres-1

# Follow all logs
docker compose logs -f

# Check database health
docker exec sciedu-local-postgres-1 pg_isready -U postgres

# List database tables
docker exec sciedu-local-postgres-1 psql -U postgres -d sciedu -c "\dt"
```

## Testing the API

```bash
# Health check
curl http://localhost:8080/api/healthz

# Should return: OK
```

## Development Workflow

1. **Make code changes** in your local workspace
2. **Rebuild and restart** the backend:
   ```bash
   docker compose up -d --build backend
   ```
3. **View logs** to verify changes:
   ```bash
   docker logs -f sciedu-backend-local
   ```

## Rebuilding Services

```bash
# Rebuild only backend (faster for code changes)
docker compose up -d --build backend

# Rebuild everything from scratch
docker compose down -v
docker compose up -d --build
```

## Stopping Services

```bash
# Stop but keep data
docker compose stop

# Stop and remove containers (data persists in volume)
docker compose down

# Stop and remove everything including data
docker compose down -v
```

## Migration Information

- Migrations are stored in `internal/database/migrations/`
- Migrations run automatically when the backend starts
- Migration history is tracked in the `schema_migrations` table
- Current migrations:
  - `1_questions.up.sql` - Questions table
  - `2_options.up.sql` - Options table  
  - `3_contents.up.sql` - Contents table
  - `6_chat.up.sql` - Chat and messages tables

## Troubleshooting

### Backend fails to start

Check the logs:
```bash
docker logs sciedu-backend-local
```

Common issues:
- PostgreSQL not healthy yet (wait for health check)
- Migration errors (check migration SQL files)
- Port 8080 already in use (stop conflicting service)

### Database connection errors

Verify PostgreSQL is running and healthy:
```bash
docker compose ps
docker exec sciedu-local-postgres-1 pg_isready -U postgres
```

### Port conflicts

If ports 5432 or 8080 are already in use:
```bash
# Find what's using the ports
lsof -i :5432
lsof -i :8080

# Either stop those services or modify ports in compose.yaml
```

## Notes

- **PostgreSQL 18.1** requires volume mount at `/var/lib/postgresql` (not `/var/lib/postgresql/data`)
- **Always run** `docker compose down -v` before starting a fresh environment to ensure clean state
- **Data persistence**: The `postgres_data` volume persists between restarts unless removed with `-v` flag
- **Build caching**: Docker caches layers for faster rebuilds; use `--no-cache` if needed
- **Network isolation**: Services communicate through the `sciedu-local` bridge network

## Files in This Directory

- `compose.yaml` - Docker Compose configuration
- `Dockerfile` - Multi-stage build for backend service
- `README.md` - This file
- `cleanup.sh` - Legacy cleanup script (use `docker compose down -v` instead)
- `deploy.sh` - Legacy deploy script  
- `start.sh` - Legacy start script
