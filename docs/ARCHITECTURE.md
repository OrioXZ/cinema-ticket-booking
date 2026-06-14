# Architecture

Status: Phase 3 implemented

## Components

- Vue 3 frontend scaffold with HTTP and WebSocket proxying
- Go + Gin REST API and WebSocket endpoint
- MongoDB for movies, showtimes, bookings, and audit logs
- Redis for temporary seat locks, Pub/Sub, and expiration notifications
- Docker Compose for the complete local stack

Package boundaries:

- `internal/booking`: Phase 2 domain, REST service, repositories, and handlers
- `internal/events`: versioned domain events, Redis publisher/subscriber, and
  seat-lock expiration listener
- `internal/audit`: asynchronous audit projection and MongoDB repository
- `internal/realtime`: public event projection, WebSocket hub, clients, and
  showtime handler
- `internal/identity`: temporary Phase 2 header identity

Firebase Authentication, admin APIs, notifications, and the booking frontend
remain deferred.

## Event Flow

1. A Redis Lua script atomically performs each public seat-state transition and
   publishes its event to `cinema.events`.
2. MongoDB insertion remains the separate durable booking success boundary;
   its subsequent Redis `BOOKED` transition is best effort.
3. Independent Redis subscribers receive the same event.
4. The audit subscriber inserts an idempotent `audit_logs` document.
5. The realtime subscriber maps state changes to public `seat.updated` messages.
6. The WebSocket hub broadcasts only to the matching showtime room.

Publisher or consumer failures do not change successful REST results. Logs omit
tokens, credentials, connection strings, and raw event payloads.

## Domain Event Contract

The internal envelope is version `1` and contains:

```text
version, id, type, occurred_at, showtime_id, seat_no, generation,
user_id, booking_id, reason
```

Event IDs use cryptographically random bytes and timestamps are UTC. Supported
types:

- `seat.locked`
- `seat.released`
- `seat.lock_expired`
- `booking.confirmed`
- `lock.acquisition_failed`

State-changing events require a positive generation. Ownership tokens are never
included.

## Realtime Contract

Endpoint:

```text
GET /ws/showtimes/:showtimeId
```

Public messages contain:

```text
type=seat.updated, event_id, showtime_id, seat_no, state, revision, occurred_at
```

Mappings:

- `seat.locked` -> `LOCKED`
- `seat.released` -> `AVAILABLE`
- `seat.lock_expired` -> `AVAILABLE`
- `booking.confirmed` -> `BOOKED`

`lock.acquisition_failed` is audited but not broadcast. User identity and
internal reasons are excluded.

The hub owns room membership under synchronization. Each connection has one
writer goroutine and a bounded send queue. Slow clients are removed instead of
blocking other clients. Read and write deadlines plus ping/pong detect dead
connections. Local browser origins are controlled by
`WEBSOCKET_ALLOWED_ORIGINS`; clients without an Origin header are allowed for
CLI and test use.

## Audit Logs

MongoDB collection `audit_logs` stores:

```text
event_id, event_type, occurred_at, processed_at,
showtime_id, seat_no, user_id, booking_id, reason
```

Indexes:

- Unique `event_id`, named `unique_event_id`
- Descending `occurred_at`, named `recent_audit_events`

Duplicate event IDs are treated as already processed.

## Lock Expiration

Redis enables keyevent expiration notifications with `Ex`. The backend
subscribes to `__keyevent@<db>__:expired`, accepts only keys shaped as
`seat_lock_expiry:{showtimeId}:{seatNo}:{generation}`. Markers expire one second
after the five-minute lock. MongoDB's durable booking check is an early safety
filter. The final Lua gate verifies the marker generation remains the active
`LOCKED` generation, no lock exists, and realtime state is not terminal
`BOOKED`; it then atomically stores `AVAILABLE` and publishes
`seat.lock_expired`. Timeout events omit `user_id`.

## Redis Seat State

Each seat uses a lock key, persistent generation key, realtime-state hash, and
generation-bearing expiration marker. Acquire, release, confirmation, and
expiration are separate Lua transitions. Each script updates Redis state and
publishes its public event atomically. Generations prevent delayed release or
expiry work from overwriting a newer generation. `BOOKED` is terminal for
Phase 3.

## Booking Correctness

Phase 2 correctness remains unchanged. Redis lock ownership protects temporary
selection, while MongoDB's unique `(showtime_id, seat_no)` index is the final
double-booking barrier. MongoDB booking insertion remains the durable
confirmation success point. The post-commit Redis `BOOKED` transition and
cleanup remain best effort; this is not a distributed transaction or
cross-system atomicity guarantee.

## Lifecycle

Startup initializes MongoDB indexes and Redis before starting:

- Audit Redis subscriber
- Realtime Redis subscriber
- Lock-expiration listener
- HTTP/WebSocket server

Each Redis worker signals readiness only after Redis confirms its subscription.
The application waits for all three signals with a bounded timeout before
opening HTTP traffic. A pre-readiness failure cancels and joins all workers and
returns through normal resource cleanup, then exits non-zero.

Shutdown stops HTTP traffic, cancels the root worker context, closes WebSocket
clients, waits for subscriptions to exit, then disconnects Redis and MongoDB.
Background runtime errors are logged without calling `log.Fatal`.

## Delivery Trade-off

Redis Pub/Sub and keyspace notifications are non-durable. Events can be missed
while the backend or a consumer is offline, and disconnected WebSocket clients
receive no backlog. Clients must reload the authoritative REST seat map after
every connect or reconnect. No polling, reconciliation, or durable broker is
implemented in Phase 3.
