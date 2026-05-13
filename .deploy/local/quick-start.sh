#!/bin/bash

# Local Development Quick Start Script
# This script provides a convenient way to start the complete local development environment

set -e

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}================================${NC}"
echo -e "${BLUE}  SciEdu Local Development${NC}"
echo -e "${BLUE}================================${NC}"
echo ""

# Clean up existing containers
echo -e "${YELLOW}→ Cleaning up previous environment...${NC}"
docker compose down -v 2>/dev/null || true
echo -e "${GREEN}✓ Cleanup complete${NC}"
echo ""

# Build and start services
echo -e "${YELLOW}→ Building and starting services...${NC}"
docker compose up -d --build

echo ""
echo -e "${YELLOW}→ Waiting for services to be healthy...${NC}"
sleep 5

# Check service status
echo ""
docker compose ps

echo ""
echo -e "${GREEN}================================${NC}"
echo -e "${GREEN}  Services Started Successfully${NC}"
echo -e "${GREEN}================================${NC}"
echo ""
echo -e "📊 PostgreSQL: ${BLUE}localhost:5432${NC}"
echo -e "   Database: ${BLUE}sciedu${NC}"
echo -e "   User: ${BLUE}postgres${NC}"
echo -e "   Password: ${BLUE}password${NC}"
echo ""
echo -e "🚀 Backend API: ${BLUE}http://localhost:8080${NC}"
echo -e "   Health Check: ${BLUE}http://localhost:8080/api/healthz${NC}"
echo ""
echo -e "Useful commands:"
echo -e "  ${YELLOW}docker compose logs -f${NC}         # View all logs"
echo -e "  ${YELLOW}docker logs -f sciedu-backend-local${NC}  # View backend logs"
echo -e "  ${YELLOW}docker compose down -v${NC}         # Stop and clean up"
echo ""

# Test health check
echo -e "${YELLOW}→ Testing API health check...${NC}"
sleep 2
HEALTH_CHECK=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/api/healthz)

if [ "$HEALTH_CHECK" = "200" ]; then
    echo -e "${GREEN}✓ API is healthy!${NC}"
else
    echo -e "${YELLOW}⚠ API health check returned: $HEALTH_CHECK${NC}"
    echo -e "${YELLOW}  Check logs: docker logs sciedu-backend-local${NC}"
fi

echo ""
