# REVNS - Reverse Nameserver Lookup

A high-performance reverse nameserver lookup system built with Go, ScyllaDB, Redis, and React.

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   React     │────▶│   Go API    │────▶│  ScyllaDB   │
│   Frontend  │     │   (Gin)     │     │  (3 nodes)  │
└─────────────┘     └─────────────┘     └─────────────┘
                           │
                           ▼
                    ┌─────────────┐
                    │    Redis    │
                    │    Cache    │
                    └─────────────┘
```

## Features

- **High Performance**: Singleflight request coalescing, Redis caching, ScyllaDB for distributed storage
- **Pagination**: Efficient cursor-based pagination for large datasets
- **Rate Limiting**: Token bucket algorithm for API protection
- **Observability**: Prometheus metrics, Zap structured logging
- **Horizontal Scaling**: Kubernetes-ready with HPA

## Quick Start

### Prerequisites

- Go 1.26+
- Docker & Docker Compose
- Node.js 20+ (for frontend)
- Python 3.8+ (for data generation)
- k6 (for load testing)

### Setup

```bash
# Start infrastructure (ScyllaDB + Redis)
make docker-up

# Generate sample data
make generate-data

# Build and ingest data
make build
make ingest

# Run the API server
make run
```

### API Usage

```bash
# Get domains by nameserver
curl http://localhost:8080/api/v1/ns/ns1.cloudflare.com?page=1&limit=100

# Health checks
curl http://localhost:8080/health/live
curl http://localhost:8080/health/ready

# Prometheus metrics
curl http://localhost:8080/metrics
```

### Frontend

```bash
# Install dependencies
cd web && npm install

# Start development server
make web-dev
```

### Kubernetes Deployment

```bash
# Deploy all components
make k8s-deploy

# Check status
kubectl get pods

# Delete all resources
make k8s-delete
```

### Load Testing

```bash
# Run k6 load tests
make load-test
```

## Project Structure

```
.
├── api/                    # Go backend
│   ├── cmd/
│   │   ├── server/         # HTTP API server
│   │   └── ingester/       # Data ingestion CLI
│   └── internal/
│       ├── cache/          # Redis client
│       ├── db/             # ScyllaDB client
│       ├── handlers/       # HTTP handlers
│       ├── middleware/     # Rate limiting, logging, metrics
│       └── models/         # Data models
├── web/                    # React frontend
│   ├── src/
│   │   ├── App.tsx         # Main application
│   │   ├── main.tsx        # Entry point with TanStack Query
│   │   └── index.css       # Tailwind styles
│   └── vite.config.ts      # Vite configuration
├── data/                   # Data generation & testing
│   ├── generate_sample_data.py
│   └── load_test.js
├── infra/                  # Infrastructure
│   ├── docker-compose.yml  # Local development
│   ├── init.cql            # ScyllaDB schema
│   └── k8s/                # Kubernetes manifests
└── Makefile               # Build automation
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/ns/:nameserver` | Get domains by nameserver |
| GET | `/health/live` | Liveness probe |
| GET | `/health/ready` | Readiness probe |
| GET | `/metrics` | Prometheus metrics |

## Query Parameters

- `page`: Page number (default: 1)
- `limit`: Results per page (default: 100, max: 1000)

## Technologies

**Backend:**
- Go 1.26
- Gin web framework
- scylla-go-driver
- go-redis/v9
- singleflight
- Zap logging
- Prometheus client

**Frontend:**
- React 18
- TypeScript
- Vite
- TailwindCSS v4
- TanStack Query
- TanStack Virtual
- Lucide icons

**Infrastructure:**
- ScyllaDB 6.2 (3-node cluster)
- Redis 7
- Kubernetes
- Docker Compose

## License

MIT
