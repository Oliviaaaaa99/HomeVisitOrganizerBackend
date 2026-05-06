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

.PHONY: build
build: ## Compile all services
	cd services/user-svc && go build -o /tmp/hvo-user-svc ./cmd/server
	cd services/property-svc && go build -o /tmp/hvo-property-svc ./cmd/server

.PHONY: test
test: ## Run all unit tests
	cd shared/go-common && go test ./...
	cd services/user-svc && go test ./...
	cd services/property-svc && go test ./...

.PHONY: tidy
tidy: ## Tidy all go modules
	cd shared/go-common && go mod tidy
	cd services/user-svc && go mod tidy
	cd services/property-svc && go mod tidy

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
