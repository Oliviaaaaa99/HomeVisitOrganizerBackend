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
├── shared/go-common/          # Cross-service Go libraries (logx, dbx, httpx, configx)
├── services/                  # User-facing services (Go on ECS Fargate in prod)
│   ├── user-svc/              # scaffolded — health + readiness wired
│   ├── property-svc/          # TODO
│   ├── media-svc/             # TODO
│   ├── availability-svc/      # TODO
│   ├── ranking-svc/           # TODO
│   └── notification-svc/      # TODO
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

# 2. Apply database migrations
make migrate

# 3. Run user-svc against the local stack
make run-user

# 4. In another terminal, hit the endpoints
curl localhost:8080/healthz       # liveness — always 200
curl localhost:8080/readyz        # readiness — pings PG + Redis
curl localhost:8080/v1/users/me   # stubbed until M1 auth lands
```

## Layout decisions (see Tech Design Doc §3 for rationale)

- **Multi-module workspace** (`go.work`): each service is its own module to keep dep graphs tight.
- **`shared/go-common/`**: cross-cutting libs (logging, DB pool, HTTP middleware, env config) — not a kitchen sink.
- **One Dockerfile per service**: built from the repo root so `shared/go-common` is in the build context.
- **Migrations live with the service** (`services/<svc>/migrations/`), applied by `golang-migrate`.

## Roadmap

| Milestone | Scope |
|-----------|-------|
| M0 (this session) | Repo skeleton + user-svc health/readiness + local docker-compose |
| M1 | Apple/Google sign-in + JWT issuance in user-svc; first real DB-backed handler |
| M2 | property-svc + media-svc with presigned upload flow |
| M3 | availability-svc + 3 rental adapters; ranking-svc with Claude Haiku stub |
| M4 | notification-svc + workers (media, retention, availability) |

See [Tech Design Doc §6](https://jlarkusm9e05atu1.usttp.larksuite.com/wiki/CBeww3AROiSaXCkJDXKuoykct4c) for the service breakdown.
