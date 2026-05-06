# HomeVisitOrganizerBackend

[![CI](https://github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/actions/workflows/ci.yml/badge.svg)](https://github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/actions/workflows/ci.yml)

Backend services for the Apartment Tour Tracker app.

See the design docs for context:
- [PRD](https://jlarkusm9e05atu1.usttp.larksuite.com/wiki/B861wUBS9iukKNkeGtfuyqOKthb)
- [Production Design Doc](https://jlarkusm9e05atu1.usttp.larksuite.com/wiki/T0zvw6KBbi10qPkc1KPuuVGwtwc)
- [Tech Design Doc](https://jlarkusm9e05atu1.usttp.larksuite.com/wiki/CBeww3AROiSaXCkJDXKuoykct4c)

## Layout

```
.
├── go.work                    # Multi-module Go workspace
├── docker-compose.yml         # Local dev stack: Postgres + Redis + LocalStack
├── Makefile                   # Common dev tasks (run `make help`)
├── shared/go-common/          # Cross-service Go libs (logx, dbx, httpx, configx, authx)
├── services/                  # User-facing services (Go on ECS Fargate in prod)
│   ├── user-svc/              # M1 — auth (Apple/Google/dev), JWT, refresh tokens
│   ├── property-svc/          # M2 — properties / units / notes CRUD
│   ├── media-svc/             # M2 — TODO (presigned uploads)
│   ├── availability-svc/      # M3 — TODO
│   ├── ranking-svc/           # M3 — TODO
│   └── notification-svc/      # M4 — TODO
└── workers/                   # Async workers (Lambda in prod)
    ├── media-worker/          # TODO
    ├── retention-sweeper/     # TODO
    └── availability-poller/   # TODO
```

## Prerequisites

- Go 1.22+
- Docker Desktop (with `docker compose`)

## Quick start

```bash
# 1. Start local infra (Postgres + Redis + LocalStack)
make up

# 2. Apply database migrations for all services
make migrate

# 3. Run the services (each in its own terminal)
make run-user      # :8080
make run-property  # :8082

# 4. Hit the endpoints
curl localhost:8080/healthz
curl localhost:8082/healthz

# Login (dev provider — no real Apple/Google needed locally)
ACCESS=$(curl -s -X POST localhost:8080/v1/auth/exchange \
  -H 'Content-Type: application/json' \
  -d '{"provider":"dev","id_token":"olivia:olivia@example.com"}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["access_token"])')

# Create a property
curl -X POST localhost:8082/v1/properties \
  -H "Authorization: Bearer $ACCESS" \
  -H 'Content-Type: application/json' \
  -d '{"address":"123 Main St","kind":"rental","latitude":37.77,"longitude":-122.41}'
```

## Endpoints

### user-svc (`:8080`)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/healthz` | — | Liveness |
| GET | `/readyz` | — | Pings PG + Redis |
| POST | `/v1/auth/exchange` | — | Verify provider id_token, issue JWT pair |
| POST | `/v1/auth/refresh` | — | Rotate refresh token |
| GET | `/v1/users/me` | JWT | Authenticated user |

### property-svc (`:8082`)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/healthz` | — | Liveness |
| GET | `/readyz` | — | Pings PG |
| POST | `/v1/properties` | JWT | Create property |
| GET | `/v1/properties` | JWT | List user's properties (`?status=`, `?kind=`, `?page=`, `?page_size=`) |
| GET | `/v1/properties/{id}` | JWT | Property detail (incl. units, notes) |
| PATCH | `/v1/properties/{id}` | JWT | Update status |
| DELETE | `/v1/properties/{id}` | JWT | Soft-archive |
| POST | `/v1/properties/{id}/units` | JWT | Add unit (Studio / 1B / 2B / 3B / Condo / etc) |
| POST | `/v1/properties/{id}/notes` | JWT | Add free-text note |

## Layout decisions (see Tech Design Doc §3 for rationale)

- **Multi-module workspace** (`go.work`): each service is its own module to keep dep graphs tight.
- **Shared library** (`shared/go-common/`): logging, DB pool, HTTP middleware, env config, auth — not a kitchen sink.
- **Per-service migrations** (`services/<svc>/migrations/`) applied by `golang-migrate`. Each service uses its own `x-migrations-table` (e.g. `user_svc_migrations`, `property_svc_migrations`) so version sequences don't collide on the shared database.
- **Auth verification** is shared via `authx.Middleware` so any service can validate JWTs that user-svc issued — no redundant secrets, no duplicated logic.
- **One Dockerfile per service**, built from the repo root so `shared/go-common` is in the build context.

## Roadmap

| Milestone | Scope | Status |
|-----------|-------|--------|
| M0 | Repo skeleton + user-svc health/readiness + local docker-compose | ✓ |
| M1 | Apple/Google sign-in + JWT issuance in user-svc; first real DB-backed handler | ✓ |
| M2 | property-svc + media-svc with presigned upload flow | property-svc ✓, media-svc next |
| M3 | availability-svc + 3 rental adapters; ranking-svc with Claude Haiku | — |
| M4 | notification-svc + workers (media, retention, availability) | — |

See [Tech Design Doc §6](https://jlarkusm9e05atu1.usttp.larksuite.com/wiki/CBeww3AROiSaXCkJDXKuoykct4c) for the service breakdown.
