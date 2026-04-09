#!/bin/bash
set -e

echo "==================================="
echo "REVNS VPS Deployment Script"
echo "==================================="

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if .env exists
if [ ! -f .env ]; then
    echo -e "${YELLOW}Warning: .env file not found. Creating from .env.example...${NC}"
    cp .env.example .env
    echo -e "${RED}Please edit .env file with your configuration before running again.${NC}"
    exit 1
fi

# Load environment variables
set -a
source .env
set +a

echo -e "${GREEN}Step 1: Building Docker images...${NC}"
docker-compose -f docker-compose.prod.yml build

echo -e "${GREEN}Step 2: Starting infrastructure...${NC}"
docker-compose -f docker-compose.prod.yml up -d scylla redis

echo -e "${GREEN}Step 3: Waiting for ScyllaDB to be ready...${NC}"
sleep 30

# Check if ScyllaDB is ready
echo "Checking ScyllaDB health..."
until docker exec revns-scylla cqlsh -e "DESCRIBE KEYSPACES" > /dev/null 2>&1; do
    echo "Waiting for ScyllaDB..."
    sleep 5
done
echo -e "${GREEN}ScyllaDB is ready!${NC}"

echo -e "${GREEN}Step 4: Starting API and Web services...${NC}"
docker-compose -f docker-compose.prod.yml up -d api web

echo -e "${GREEN}Step 5: Checking service health...${NC}"
sleep 10

# Health checks
API_HEALTH=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/health/live || echo "000")
WEB_HEALTH=$(curl -s -o /dev/null -w "%{http_code}" http://localhost/ || echo "000")

if [ "$API_HEALTH" = "200" ]; then
    echo -e "${GREEN}✓ API is healthy${NC}"
else
    echo -e "${RED}✗ API health check failed (status: $API_HEALTH)${NC}"
fi

if [ "$WEB_HEALTH" = "200" ] || [ "$WEB_HEALTH" = "304" ]; then
    echo -e "${GREEN}✓ Web frontend is healthy${NC}"
else
    echo -e "${RED}✗ Web health check failed (status: $WEB_HEALTH)${NC}"
fi

echo ""
echo -e "${GREEN}===================================${NC}"
echo -e "${GREEN}Deployment Complete!${NC}"
echo -e "${GREEN}===================================${NC}"
echo ""
echo "Access your application:"
echo "  - Frontend: http://localhost (or your domain)"
echo "  - API:      http://localhost:8080"
echo "  - Metrics:  http://localhost:9090 (Prometheus)"
echo "  - Grafana:  http://localhost:3000"
echo ""
echo "Useful commands:"
echo "  View logs:     docker-compose -f docker-compose.prod.yml logs -f"
echo "  Stop:          docker-compose -f docker-compose.prod.yml down"
echo "  Restart:       docker-compose -f docker-compose.prod.yml restart"
echo "  Update:        ./deploy.sh"
echo ""
