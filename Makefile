# Sentinel RMM Platform - Makefile
# Provides common commands for development and deployment

.PHONY: help dev build test clean deploy agent

# Default target
help:
	@echo "Sentinel RMM Platform - Available Commands"
	@echo ""
	@echo "Development:"
	@echo "  make dev          - Start development environment"
	@echo "  make build        - Build all containers"
	@echo "  make test         - Run tests"
	@echo "  make clean        - Clean build artifacts"
	@echo ""
	@echo "Deployment:"
	@echo "  make deploy       - Deploy to production"
	@echo "  make logs         - View container logs"
	@echo "  make status       - Check service status"
	@echo ""
	@echo "Agent:"
	@echo "  make agent        - Build agent for current platform"
	@echo "  make agent-all    - Build agent for all platforms"
	@echo ""
	@echo "Database:"
	@echo "  make migrate      - Run database migrations"
	@echo "  make db-shell     - Open database shell"
	@echo ""

# Development
dev:
	docker-compose -f docker-compose.yml -f docker-compose.dev.yml up --build

dev-bg:
	docker-compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build

# Build
build:
	docker-compose build

build-no-cache:
	docker-compose build --no-cache

# Production
up:
	docker-compose up -d

down:
	docker-compose down

restart:
	docker-compose restart

# Logs
logs:
	docker-compose logs -f

logs-backend:
	docker-compose logs -f backend

logs-frontend:
	docker-compose logs -f frontend

# Status
status:
	docker-compose ps

# Clean
clean:
	docker-compose down -v --remove-orphans
	docker system prune -f
	rm -rf frontend/node_modules
	rm -rf frontend/dist
	rm -rf agent/target

# Deploy
deploy:
	./scripts/deploy.sh

# Agent builds
agent:
	cd agent && cargo build --release

agent-all:
	./scripts/build-agent.sh

agent-windows:
	cd agent && cargo build --release --target x86_64-pc-windows-gnu

agent-linux:
	cd agent && cargo build --release --target x86_64-unknown-linux-gnu

# Database
migrate:
	docker-compose exec backend ./sentinel migrate

db-shell:
	docker-compose exec postgres psql -U sentinel -d sentinel

db-backup:
	docker-compose exec postgres pg_dump -U sentinel sentinel > backup_$(shell date +%Y%m%d_%H%M%S).sql

# Testing
test:
	cd server && go test ./...
	cd frontend && npm test

test-backend:
	cd server && go test -v ./...

test-frontend:
	cd frontend && npm test

# Linting
lint:
	cd server && golangci-lint run
	cd frontend && npm run lint

# Generate
generate:
	cd server && go generate ./...

# Security scan
security:
	docker scan sentinel-backend
	cd frontend && npm audit

# Shell access
shell-backend:
	docker-compose exec backend sh

shell-frontend:
	docker-compose exec frontend sh

shell-redis:
	docker-compose exec redis redis-cli
