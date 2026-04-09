.PHONY: all build test run docker-up docker-down k8s-deploy k8s-delete ingest load-test clean

# Variables
API_DIR = ./api
WEB_DIR = ./web
DATA_DIR = ./data
INFRA_DIR = ./infra

# Default target
all: build

# Build Go binaries
build:
	@echo "Building Go binaries..."
	cd $(API_DIR) && go build -o ../bin/server ./cmd/server
	cd $(API_DIR) && go build -o ../bin/ingester ./cmd/ingester
	@echo "Build complete!"

# Run tests
test:
	@echo "Running tests..."
	cd $(API_DIR) && go test ./...
	@echo "Tests complete!"

# Start infrastructure (Docker)
docker-up:
	@echo "Starting infrastructure with Docker Compose..."
	cd $(INFRA_DIR) && docker-compose up -d
	@echo "Waiting for ScyllaDB to be ready..."
	sleep 30

# Stop infrastructure
docker-down:
	@echo "Stopping infrastructure..."
	cd $(INFRA_DIR) && docker-compose down

# Generate sample data
generate-data:
	@echo "Generating sample data..."
	cd $(DATA_DIR) && python3 generate_sample_data.py -n 100000 -o domains.csv

# Ingest data
ingest: build
	@echo "Ingesting data..."
	./bin/ingester -csv $(DATA_DIR)/domains.csv -scylla 127.0.0.1 -redis 127.0.0.1:6379

# Run the API server
run: build
	@echo "Starting API server..."
	./bin/server

# Run load tests with k6
load-test:
	@echo "Running load tests..."
	k6 run $(DATA_DIR)/load_test.js

# Kubernetes deployment
k8s-deploy:
	@echo "Deploying to Kubernetes..."
	kubectl apply -f $(INFRA_DIR)/k8s/scylla.yaml
	kubectl apply -f $(INFRA_DIR)/k8s/redis.yaml
	kubectl apply -f $(INFRA_DIR)/k8s/api.yaml
	kubectl apply -f $(INFRA_DIR)/k8s/web.yaml
	@echo "Deployment complete!"

k8s-delete:
	@echo "Deleting Kubernetes resources..."
	kubectl delete -f $(INFRA_DIR)/k8s/

# Web frontend
web-dev:
	@echo "Starting web development server..."
	cd $(WEB_DIR) && npm run dev

web-build:
	@echo "Building web frontend..."
	cd $(WEB_DIR) && npm run build

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	cd $(WEB_DIR) && rm -rf dist/

# Full setup
setup: docker-up generate-data ingest
	@echo "Setup complete! You can now run 'make run' to start the API server."
