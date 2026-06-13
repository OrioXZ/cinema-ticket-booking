# Cinema Ticket Booking

Phase 2 implementation of a cinema ticket booking take-home assignment. The
application provides the core cinema domain, five-minute Redis seat locks,
durable booking confirmation, concurrency protection, and Docker Compose setup.

Firebase Authentication, realtime updates, Redis Pub/Sub, audit consumers,
notifications, admin APIs, and the booking UI are intentionally deferred.

## Technology

- Go 1.24 with Gin
- Vue 3, TypeScript, and Vite
- MongoDB 8
- Redis 8
- Nginx
- Docker Compose

## Quick Start

```bash
docker compose up --build
```

Services:

- Frontend: <http://localhost:5173>
- Backend health: <http://localhost:8080/health>
- Proxied health: <http://localhost:5173/api/health>

Compose has development defaults. Copy `.env.example` to `.env` only when
customizing them. Stop the stack with `docker compose down`.

## Seeded Data

Startup idempotently upserts movie `movie-1` and showtime `showtime-1`. The
showtime contains seats `A1` through `A10` and `B1` through `B10`.

## Phase 2 API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/showtimes` | List showtimes and movies |
| `GET` | `/api/showtimes/:showtimeId/seats` | Resolve all seat states |
| `POST` | `/api/showtimes/:showtimeId/seats/:seatNo/lock` | Acquire a lock |
| `DELETE` | `/api/showtimes/:showtimeId/seats/:seatNo/lock` | Release an owned lock |
| `POST` | `/api/bookings/confirm` | Confirm an owned lock as a booking |
| `GET` | `/api/bookings/me` | List the current user's bookings |

Mutations and personal bookings use `X-User-ID` as temporary Phase 2 identity.
`X-User-Role` is parsed for later phases but no admin API exists yet. Identity
from request bodies is never trusted. Firebase verification replaces these
headers in Phase 4.

Errors use:

```json
{
  "error": {
    "code": "SEAT_CONFLICT",
    "message": "the seat is already locked or booked"
  }
}
```

Example lock and confirmation:

```powershell
$lock = Invoke-RestMethod `
  -Method Post `
  -Uri http://localhost:8080/api/showtimes/showtime-1/seats/A1/lock `
  -Headers @{"X-User-ID" = "demo-user"}

$body = @{
  showtime_id = "showtime-1"
  seat_no = "A1"
  ownership_token = $lock.ownership_token
} | ConvertTo-Json

Invoke-RestMethod `
  -Method Post `
  -Uri http://localhost:8080/api/bookings/confirm `
  -Headers @{"X-User-ID" = "demo-user"} `
  -ContentType "application/json" `
  -Body $body
```

## Lock and Booking Correctness

Redis locks use:

```text
key:   seat_lock:{showtimeId}:{seatNo}
value: {"user_id":"...","ownership_token":"..."}
TTL:   5 minutes
```

Acquisition uses Redis `SET NX` with expiration. Ownership tokens contain 256
random bits from `crypto/rand`; user ID alone cannot release or confirm a lock.
Release uses one Lua compare-and-delete operation. Seat maps use `MGET` for the
configured seats and never use Redis `KEYS`.

Confirmation validates the showtime and seat, verifies both lock owner fields,
atomically refreshes the exact owner's five-minute lease, inserts a `CONFIRMED`
booking, and safely releases the lock. MongoDB's unique compound index on
`(showtime_id, seat_no)` is the final double-booking barrier. Duplicate keys
return HTTP `409`.

Redis and MongoDB do not share a transaction. A crash after MongoDB commits but
before Redis cleanup can leave a lock until its TTL expires, and a lock can
expire during an in-flight confirmation. Neither case permits two durable
bookings because the MongoDB unique index remains authoritative.

## Local Development

```powershell
docker compose up -d mongodb redis
cd backend
$env:MONGO_HOST = "127.0.0.1:27017"
$env:MONGO_DATABASE = "cinema"
$env:MONGO_USERNAME = "cinema"
$env:MONGO_PASSWORD = "cinema_dev_password"
$env:REDIS_URI = "redis://127.0.0.1:6379/0"
go run ./cmd/api
```

Run the frontend separately with `npm install` and `npm run dev` from
`frontend`. Vite preserves `/api` when proxying to port `8080`.

## Validation

Backend:

```powershell
cd backend
gofmt -w cmd internal
go mod tidy
go test ./...
go build -o bin/api.exe ./cmd/api
cd ..
```

Opt-in real MongoDB/Redis integration and concurrency test:

```powershell
docker compose up -d mongodb redis
cd backend
$env:MONGO_URI = "mongodb://cinema:cinema_dev_password@127.0.0.1:27017/?authSource=admin"
$env:REDIS_URI = "redis://127.0.0.1:6379/15"
go test -tags=integration ./internal/booking
cd ..
```

The integration test drops only `cinema_phase2_integration` and removes its
known Redis lock keys.

Frontend and Compose:

```powershell
cd frontend
npm.cmd ci
npm.cmd run type-check
npm.cmd run build
cd ..
docker compose config --quiet
docker compose up --build -d
docker compose ps
curl.exe --fail http://localhost:8080/health
curl.exe --fail http://localhost:5173/api/health
docker compose down
```

## Project Structure

```text
backend/internal/booking/   Domain, service, handlers, MongoDB and Redis adapters
backend/internal/identity/  Temporary Phase 2 request identity
backend/internal/health/    Dependency-aware health endpoint
frontend/                   Vue scaffold and API proxies
docs/                       Assignment and architecture notes
```

## Current Limitations

- Development headers are not secure authentication.
- No WebSocket, Pub/Sub, realtime broadcast, audit, notification, or admin API.
- The Vue screen remains the infrastructure status page; booking UI is Phase 5.
- Payment is the mock confirmation action.
- Redis/MongoDB confirmation is not a cross-system transaction.
