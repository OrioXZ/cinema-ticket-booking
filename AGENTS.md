# AGENTS.md

## Project

This repository contains a take-home assignment for a cinema ticket booking system.

The goal is a small, correct, and clearly documented MVP. Prioritize concurrency correctness, prevention of double booking, reproducible Docker setup, and clear architectural reasoning over UI polish or unnecessary features.

Read `docs/ASSIGNMENT.md` before planning or implementing work.

## Mandatory technology stack

- Backend: Go with Gin
- Frontend: Vue 3 with TypeScript and Vite
- Database: MongoDB
- Distributed seat lock: Redis
- Realtime updates: WebSocket
- Message queue/event transport: Redis Pub/Sub
- Authentication: Firebase Authentication with Google sign-in
- Deployment: Docker and Docker Compose

The complete system must start with:

```bash
docker compose up --build
```

Do not replace a mandatory technology without explicit user approval.

## Core invariants

- A seat must never be booked more than once for the same showtime.
- Seat locks expire after five minutes.
- A seat lock must have an ownership token, not only a user ID.
- Only the lock owner may confirm or release a lock.
- Lock release and lock extension must verify ownership atomically.
- MongoDB must enforce a unique index for the combination of showtime and seat.
- Other connected users must receive seat-state changes in realtime.
- Admin endpoints must reject normal users.
- Redis Pub/Sub must serve a real use case and must not exist unused.
- Configuration and secrets must come from environment variables.

## Scope

Keep the implementation intentionally small:

- One or two seeded movies/showtimes are sufficient.
- A simple seat grid is sufficient.
- Payment is a mock confirmation action.
- Admin UI may be a simple booking table with one useful filter.
- Production-level visual design is not required.

Do not add unrelated features unless requested.

## Engineering expectations

- Use clear package boundaries and meaningful names.
- Keep HTTP handlers thin.
- Put business logic in services.
- Keep persistence and Redis operations behind repository or adapter interfaces where practical.
- Return structured API errors with appropriate status codes.
- Avoid global mutable application state.
- Do not hardcode credentials, hostnames, IDs, or ports that should be configurable.
- Add dependencies only when they materially simplify the implementation.
- Update documentation when behavior or architecture changes.

## Working process

Before implementing a substantial task:

1. Read the relevant files.
2. State the intended changes and important assumptions.
3. Identify concurrency or security risks.
4. Implement the smallest complete change.
5. Format, lint, build, and test affected components.
6. Report commands run, results, and unresolved limitations.

Do not silently ignore failing tests or build errors.

## Validation commands

Run Go formatting, tests, and build:

```powershell
cd backend
gofmt -w cmd internal
go test ./...
go build -o bin/api.exe ./cmd/api
cd ..
```

Install frontend dependencies, type check, and build:

```powershell
cd frontend
npm.cmd ci
npm.cmd run type-check
npm.cmd run build
cd ..
```

Validate Docker Compose configuration:

```powershell
docker compose config --quiet
```

Build and start the full stack, verify health, and stop it:

```powershell
docker compose up --build -d
docker compose ps
curl.exe --fail http://localhost:8080/health
curl.exe --fail http://localhost:5173/api/health
docker compose down
```

## Git behavior

- Make focused changes.
- Do not rewrite unrelated code.
- Do not commit secrets or local environment files.
- Keep `.env.example` synchronized with required configuration.
