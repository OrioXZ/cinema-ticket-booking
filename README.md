# Cinema Ticket Booking

Phase 4 implementation of a cinema ticket booking take-home assignment. The
application provides the core cinema domain, five-minute Redis seat locks,
durable booking confirmation, Redis Pub/Sub events, realtime WebSocket seat
updates, asynchronous MongoDB audit logs, Firebase authentication support,
role authorization, an admin bookings API, and Docker Compose setup.

The final login, booking, and admin screens remain deferred to Phase 5.

## Technology

- Go 1.24 with Gin
- Vue 3, TypeScript, and Vite
- MongoDB 8
- Redis 8
- Redis Pub/Sub and keyspace expiration notifications
- WebSocket realtime updates
- Firebase Authentication and Firebase Admin token verification
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
customizing them. `MONGO_DATABASE` is always required by the backend, including
when `MONGO_URI` is supplied; Compose explicitly provides its development
database value. Authentication defaults explicitly to local development mode,
so evaluators can start the system without owning a Firebase project. Stop the
stack with `docker compose down`.

## Seeded Data

Startup idempotently upserts movie `movie-1` and showtime `showtime-1`. The
showtime contains seats `A1` through `A10` and `B1` through `B10`.

## API And Authorization

| Access | Method and path | Purpose |
| --- | --- | --- |
| Public | `GET /health`, `GET /api/health` | Dependency health |
| Public | `GET /api/showtimes` | List showtimes and movies |
| Public | `GET /api/showtimes/:showtimeId/seats` | Resolve all seat states |
| Public | `GET /ws/showtimes/:showtimeId` | Public seat-state WebSocket |
| `USER` or `ADMIN` | `POST /api/showtimes/:showtimeId/seats/:seatNo/lock` | Acquire a lock |
| `USER` or `ADMIN` | `DELETE /api/showtimes/:showtimeId/seats/:seatNo/lock` | Release an owned lock |
| `USER` or `ADMIN` | `POST /api/bookings/confirm` | Confirm an owned lock |
| `USER` or `ADMIN` | `GET /api/bookings/me` | List the current user's bookings |
| `ADMIN` | `GET /api/admin/bookings` | List confirmed bookings |

The admin endpoint accepts exact `user_id` filtering and a `limit` from 1 to
100 (default 50). Results are newest first:

```text
GET /api/admin/bookings?user_id=firebase-uid&limit=25
```

Booking identity always comes from authenticated middleware. Request bodies
cannot override user ID or role, and admin/WebSocket responses never expose
Redis ownership tokens or Firebase credentials.

## Authentication

The backend supports two explicit modes:

- `AUTH_MODE=development` reads `X-User-ID` and `X-User-Role`. A missing role
  defaults to `USER`; only `USER` and `ADMIN` are accepted. This mode is an
  intentionally insecure local convenience for reproducible one-command
  Docker startup.
- `AUTH_MODE=firebase` requires `Authorization: Bearer <Firebase ID token>`.
  The official Firebase Admin SDK verifies the token and supplies the UID.
  Development identity headers are ignored.

Firebase mode maps the verified custom claim `role`. Exact `ADMIN` becomes
`ADMIN`; all missing, malformed, unknown, or `USER` values become `USER`.
Administrators must be assigned with trusted Firebase Admin tooling outside
this public application, for example:

```javascript
await getAuth().setCustomUserClaims(uid, { role: 'ADMIN' })
```

There is no self-promotion endpoint or database-backed user-management system.

The browser flow prepared for Phase 5 is:

```text
Google sign-in -> Firebase Auth -> current Firebase ID token
-> Authorization bearer header -> Gin auth middleware
-> verified application Identity -> booking/admin handler
```

Configure real backend Firebase mode with Application Default Credentials:

```text
AUTH_MODE=firebase
FIREBASE_PROJECT_ID=your-project-id
GOOGLE_APPLICATION_CREDENTIALS=/run/secrets/firebase-service-account.json
```

Mount the credential file at runtime with a local Compose override or provide
another supported Application Default Credentials source. Never commit the
service-account JSON, place it in the Docker build context, or expose it
through `VITE_*`. Firebase mode fails startup when its project or credentials
cannot initialize; it never falls back to development mode.

Frontend Firebase web configuration is public application configuration but
still comes from environment variables:

```text
VITE_AUTH_MODE=firebase
VITE_FIREBASE_API_KEY=
VITE_FIREBASE_AUTH_DOMAIN=
VITE_FIREBASE_PROJECT_ID=
VITE_FIREBASE_APP_ID=
```

The frontend foundation provides Google popup sign-in, sign-out, auth-state
observation, current ID-token retrieval, and an API client that attaches a
fresh bearer token. Tokens are not written to `localStorage`. Development
builds use `VITE_DEV_USER_ID` and `VITE_DEV_USER_ROLE` only for explicit local
requests.

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
  -Headers @{"X-User-ID" = "demo-user"; "X-User-Role" = "USER"}

$body = @{
  showtime_id = "showtime-1"
  seat_no = "A1"
  ownership_token = $lock.ownership_token
} | ConvertTo-Json

Invoke-RestMethod `
  -Method Post `
  -Uri http://localhost:8080/api/bookings/confirm `
  -Headers @{"X-User-ID" = "demo-user"; "X-User-Role" = "USER"} `
  -ContentType "application/json" `
  -Body $body
```

## Lock and Booking Correctness

Redis locks use:

```text
seat_lock:{showtimeId}:{seatNo}
seat_lock_generation:{showtimeId}:{seatNo}
seat_realtime_state:{showtimeId}:{seatNo}
seat_lock_expiry:{showtimeId}:{seatNo}:{generation}
```

The lock value contains the user ID, ownership token, and an internal
generation. The REST response omits generation. Locks keep the five-minute TTL;
the generation-bearing expiry marker lasts one additional second so Redis has
removed the real lock before timeout processing begins. Ownership tokens
contain 256 random bits from `crypto/rand`; user ID alone cannot release or
confirm a lock. Seat maps use `MGET` for configured seats and never use Redis
`KEYS`.

Redis Lua scripts atomically combine each public state mutation with its
Pub/Sub event. Acquire stores `LOCKED`; release stores `AVAILABLE`;
confirmation stores terminal `BOOKED`; true expiration stores `AVAILABLE`.
An old release or expiry generation cannot overwrite a newer lock. After
`BOOKED`, later acquire, release, and expiration scripts cannot publish
`LOCKED` or `AVAILABLE`.

Confirmation validates the showtime and seat and atomically compares both lock
owner fields without changing the lock's remaining TTL. A correct owner can
confirm only during the original five-minute window; repeated attempts never
refresh it. MongoDB then inserts the `CONFIRMED` booking. Its unique compound
index on `(showtime_id, seat_no)` is the final double-booking barrier, and
duplicate keys return HTTP `409`.

Successful MongoDB insertion is the durable success boundary. The post-commit
Redis `BOOKED` transition and lock cleanup are best effort: an error is logged
and the API still returns the confirmed booking. Redis and MongoDB do not share
a transaction, so this is not cross-system atomicity. These failures cannot
permit two durable bookings because MongoDB remains authoritative.

## Realtime Events

Phase 3 publishes a versioned internal event envelope to Redis channel
`cinema.events`:

```json
{
  "version": 1,
  "id": "random-event-id",
  "type": "seat.locked",
  "occurred_at": "2026-06-14T12:00:00Z",
  "showtime_id": "showtime-1",
  "seat_no": "A1",
  "generation": 42,
  "user_id": "user-1",
  "booking_id": "",
  "reason": ""
}
```

Event types are:

- `seat.locked`
- `seat.released`
- `seat.lock_expired`
- `booking.confirmed`
- `lock.acquisition_failed`

Publishing and consumption are best effort. Redis Pub/Sub is non-durable, so
events may be missed while the application or client is offline. REST booking
and lock correctness remains authoritative.

State-changing events require a positive generation.
`lock.acquisition_failed` may omit it because no state transition occurred.

Two Redis subscribers consume `cinema.events` independently:

- The realtime consumer projects state-changing events to WebSocket rooms.
- The audit consumer inserts asynchronous records into MongoDB `audit_logs`.

WebSocket clients connect to one showtime:

```text
ws://localhost:8080/ws/showtimes/showtime-1
ws://localhost:5173/ws/showtimes/showtime-1
```

Public messages never contain user identity:

```json
{
  "type": "seat.updated",
  "event_id": "random-event-id",
  "showtime_id": "showtime-1",
  "seat_no": "A1",
  "state": "LOCKED",
  "revision": 42,
  "occurred_at": "2026-06-14T12:00:00Z"
}
```

Possible public states are `LOCKED`, `AVAILABLE`, and `BOOKED`. Revision is the
Redis seat generation and allows clients to ignore duplicate or older
messages. After connecting or reconnecting, clients must reload the REST seat
map because Pub/Sub and WebSocket delivery are transient.

## Lock Expiration

Redis runs with `notify-keyspace-events Ex`. The backend subscribes to
`__keyevent@0__:expired` and filters
`seat_lock_expiry:{showtimeId}:{seatNo}:{generation}` markers. MongoDB is an
early durable-booking safety check. The final Redis Lua gate requires the same
generation to remain `LOCKED`, no current lock, and a non-`BOOKED` realtime
state before atomically storing `AVAILABLE` and publishing
`seat.lock_expired`. Marker expiry adds roughly one second to realtime timeout
notification. Stale markers cannot overwrite a newer lock or booking.

Expiration events can still be missed while the backend is offline because
Redis Pub/Sub and keyspace notifications are non-durable. Clients reload the
authoritative REST seat map after reconnecting. No polling or reconciliation
worker exists in Phase 3.

## Audit Logs

The `audit_logs` collection records booking confirmation, manual release, lock
expiration, and lock acquisition failure. It has:

- Unique index `unique_event_id` on `event_id`
- Index `recent_audit_events` on `occurred_at` descending

Inspect recent audit entries:

```powershell
docker compose exec mongodb mongosh `
  --username cinema `
  --password cinema_dev_password `
  --authenticationDatabase admin `
  cinema `
  --eval "db.audit_logs.find().sort({occurred_at:-1}).limit(20).toArray()"
```

Audit writes happen after the originating HTTP request through Redis Pub/Sub.
Duplicate event delivery is idempotent by `event_id`.

## Graceful Shutdown

The backend owns a root application context for the HTTP server, audit
subscriber, realtime subscriber, and expiration listener. HTTP starts only
after Redis confirms all three subscriptions; startup has a bounded timeout
and cancels and joins every worker on failure. Startup failures run deferred
MongoDB and Redis cleanup and exit non-zero. Shutdown stops HTTP traffic,
cancels subscriptions, closes WebSocket clients, waits for workers, and only
then closes Redis and MongoDB. Pub/Sub remains non-durable after readiness.

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
`frontend`. Set `AUTH_MODE=development` and `VITE_AUTH_MODE=development` for
the local header adapter. Vite preserves `/api`, `Authorization`, and
development identity headers when proxying to port `8080`.

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
$env:MONGO_DATABASE = "cinema"
$env:REDIS_URI = "redis://127.0.0.1:6379/15"
go test -tags=integration ./internal/...
cd ..
```

The integration test drops only `cinema_phase2_integration` and removes its
known Redis lock keys. Phase 3 integration tests also use isolated audit
databases and Redis channels.

To test realtime updates manually, connect one or more WebSocket clients to a
showtime URL, then run the lock, release, and confirmation REST requests above.
Expect `LOCKED`, `AVAILABLE`, and `BOOKED` messages respectively. A client in a
different showtime room must receive none of those messages.

## Postman Collection

The ordered Phase 4 development-auth workflow is available in `postman/`.

1. Reset local state with `docker compose down --volumes`.
2. Start the stack with `docker compose up --build`.
3. Import both JSON files from `postman/` into Postman.
4. Select the `Cinema Local` environment.
5. Run the `Cinema Ticket Booking` collection in order.
6. Use clean seeded data; the workflow expects all seats to begin as
   `AVAILABLE`.

The collection validates health, catalog, seat maps, authentication and
validation errors, lock ownership, booking confirmation, admin rejection, an
authorized admin listing, and the `user_id` filter. `firebaseIdToken` is an
empty convenience variable; in Firebase mode use
`Authorization: Bearer {{firebaseIdToken}}` without committing a real token.

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
backend/internal/audit/     Asynchronous MongoDB audit consumer
backend/internal/booking/   Domain, service, handlers, MongoDB and Redis adapters
backend/internal/events/    Event contract, Redis transport, expiration listener
backend/internal/identity/  Auth identity, roles, middleware, and Firebase adapter
backend/internal/realtime/  WebSocket hub, clients, and public event projection
backend/internal/health/    Dependency-aware health endpoint
frontend/                   Vue scaffold and API proxies
docs/                       Assignment and architecture notes
```

## Current Limitations

- Development headers are not secure authentication and must not be used in production.
- Firebase administrator claims are provisioned outside this application.
- No notification delivery or database-backed user management.
- The Vue screen remains the infrastructure status page; booking UI is Phase 5.
- The Phase 5 login screen, route guards, seat map, lock countdown, mock
  payment screen, and admin table are not implemented.
- Payment is the mock confirmation action.
- Redis/MongoDB confirmation is not a cross-system transaction.
- A failed post-commit Redis `BOOKED` transition has no reconciliation worker;
  the REST seat map still resolves the durable MongoDB booking as `BOOKED`.
- Redis Pub/Sub, WebSocket updates, and expiration notifications are transient.
