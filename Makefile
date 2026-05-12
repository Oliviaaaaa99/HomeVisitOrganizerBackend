# Common dev tasks. Run `make help` for a list.

.DEFAULT_GOAL := help

POSTGRES_URL ?= postgres://hvo:hvo_dev@localhost:5432/hvo?sslmode=disable
REDIS_URL    ?= redis://localhost:6379/0
JWT_SECRET   ?= dev-secret-not-for-prod-12345678901234

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

.PHONY: up
up: ## Start local infra (Postgres, Redis, LocalStack)
	docker compose up -d postgres redis localstack
	@echo "Waiting for services to be healthy..."
	@for s in postgres redis localstack; do \
		until [ "$$(docker compose ps -q $$s | xargs -I{} docker inspect -f '{{.State.Health.Status}}' {})" = "healthy" ]; do sleep 1; done; \
		echo "  $$s ✓"; \
	done

.PHONY: down
down: ## Stop local infra
	docker compose down

.PHONY: nuke
nuke: ## Stop and remove all data volumes (DESTRUCTIVE)
	docker compose down -v

.PHONY: migrate
migrate: ## Apply migrations for ALL services
	docker compose run --rm migrate

.PHONY: migrate-user-down
migrate-user-down: ## Roll back the last user-svc migration
	docker compose run --rm migrate \
		/migrate -path=/migrations/user-svc \
		-database='postgres://hvo:hvo_dev@postgres:5432/hvo?sslmode=disable&x-migrations-table=user_svc_migrations' \
		down 1

.PHONY: migrate-property-down
migrate-property-down: ## Roll back the last property-svc migration
	docker compose run --rm migrate \
		/migrate -path=/migrations/property-svc \
		-database='postgres://hvo:hvo_dev@postgres:5432/hvo?sslmode=disable&x-migrations-table=property_svc_migrations' \
		down 1

.PHONY: migrate-media-down
migrate-media-down: ## Roll back the last media-svc migration
	docker compose run --rm migrate \
		/migrate -path=/migrations/media-svc \
		-database='postgres://hvo:hvo_dev@postgres:5432/hvo?sslmode=disable&x-migrations-table=media_svc_migrations' \
		down 1

# Apply migrations to a REMOTE database (Neon, RDS, etc). Reads DATABASE_URL
# from the environment so secrets never land in the Makefile.
#
# Usage:
#   DATABASE_URL='postgres://user:pass@ep-xyz.neon.tech/hvo?sslmode=require' \
#     make migrate-remote
#
# Runs each service's migrations in turn into its own table
# (foo_svc_migrations) so they don't collide.
.PHONY: migrate-remote
migrate-remote: ## Apply migrations against $$DATABASE_URL (Neon, RDS, …)
	@if [ -z "$$DATABASE_URL" ]; then \
		echo "DATABASE_URL is required (e.g. from Neon)"; exit 2; \
	fi
	@command -v migrate >/dev/null 2>&1 || { \
		echo "migrate CLI not installed. Install: brew install golang-migrate"; exit 2; \
	}
	@set -e; for svc in user-svc property-svc media-svc ranking-svc; do \
		tbl=$$(echo "$$svc" | tr '-' '_')_migrations; \
		echo ">>> migrate $$svc (table=$$tbl)"; \
		migrate -path "./services/$$svc/migrations" \
			-database "$$DATABASE_URL&x-migrations-table=$$tbl" \
			up; \
	done

.PHONY: build
build: ## Compile all services
	cd services/user-svc && go build -o /tmp/hvo-user-svc ./cmd/server
	cd services/property-svc && go build -o /tmp/hvo-property-svc ./cmd/server
	cd services/media-svc && go build -o /tmp/hvo-media-svc ./cmd/server
	cd services/ranking-svc && go build -o /tmp/hvo-ranking-svc ./cmd/server

.PHONY: test
test: ## Run all unit tests
	cd shared/go-common && go test ./...
	cd services/user-svc && go test ./...
	cd services/property-svc && go test ./...
	cd services/media-svc && go test ./...
	cd services/ranking-svc && go test ./...

.PHONY: tidy
tidy: ## Tidy all go modules
	cd shared/go-common && go mod tidy
	cd services/user-svc && go mod tidy
	cd services/property-svc && go mod tidy
	cd services/media-svc && go mod tidy
	cd services/ranking-svc && go mod tidy

.PHONY: run-user
run-user: ## Run user-svc against local infra
	cd services/user-svc && \
		ENV=dev \
		HTTP_ADDR=':8080' \
		JWT_SECRET='$(JWT_SECRET)' \
		DATABASE_URL='$(POSTGRES_URL)' REDIS_URL='$(REDIS_URL)' \
		go run ./cmd/server

.PHONY: run-property
run-property: ## Run property-svc against local infra
	cd services/property-svc && \
		HTTP_ADDR=':8082' \
		JWT_SECRET='$(JWT_SECRET)' \
		DATABASE_URL='$(POSTGRES_URL)' \
		go run ./cmd/server

# Pick the Mac's WiFi IP so presigned URLs are reachable from a phone on the
# same network. Falls back to localhost if no LAN.
LAN_IP ?= $(shell ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo localhost)

.PHONY: run-media
run-media: ## Run media-svc against local infra (uses LocalStack S3, iPhone-reachable URLs)
	@echo "Using AWS_ENDPOINT_URL=http://$(LAN_IP):4566 (iPhone reachable)"
	cd services/media-svc && \
		HTTP_ADDR=':8083' \
		JWT_SECRET='$(JWT_SECRET)' \
		DATABASE_URL='$(POSTGRES_URL)' \
		AWS_REGION=us-east-1 \
		AWS_ENDPOINT_URL=http://$(LAN_IP):4566 \
		AWS_ACCESS_KEY_ID=test \
		AWS_SECRET_ACCESS_KEY=test \
		AWS_S3_PATH_STYLE=true \
		S3_BUCKET=hvo-media-dev \
		S3_AUTO_CREATE_BUCKET=true \
		go run ./cmd/server

.PHONY: run-ranking
run-ranking: ## Run ranking-svc against local infra
	cd services/ranking-svc && \
		HTTP_ADDR=':8084' \
		JWT_SECRET='$(JWT_SECRET)' \
		DATABASE_URL='$(POSTGRES_URL)' \
		go run ./cmd/server
