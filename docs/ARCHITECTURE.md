# Architecture

Status: Phase 2 implemented

## Components

- Vue 3 frontend scaffold and API proxy
- Go + Gin backend
- MongoDB for movies, showtimes, and bookings
- Redis for temporary distributed seat locks
- Docker Compose for the complete local stack

`internal/booking` contains domain models, business services, Gin handlers,
MongoDB persistence, and the Redis lock adapter. `internal/identity` provides
temporary Phase 2 header identity. Handlers stay thin; repositories and lock
operations are interfaces so service behavior can be tested deterministically.

WebSocket, Redis Pub/Sub, Firebase Authentication, audit processing,
notifications, and admin APIs remain deferred.

## Data Model

- `movies`: stable seeded movie documents
- `showtimes`: stable seeded showtimes with ordered seat definitions
- `bookings`: durable confirmed bookings
- Redis only: active seat locks

Movie `movie-1` and showtime `showtime-1` are replaced with upsert semantics on
startup. This keeps seed data deterministic and idempotent.

MongoDB indexes:

- Unique `bookings(showtime_id, seat_no)`, named `unique_showtime_seat`
- `bookings(user_id, created_at desc)`, named `bookings_by_user`

## Booking Flow

1. A Phase 2 client supplies `X-User-ID`; request-body identity is ignored.
2. The service validates the showtime and configured seat.
3. It checks durable booking state.
4. Redis `SET NX` creates a five-minute lock with a random ownership token.
5. Durable booking state is checked again after acquisition.
6. Confirmation atomically compares the user ID and token without changing the
   original five-minute TTL.
7. MongoDB inserts the `CONFIRMED` booking.
8. The unique index rejects any competing booking for that seat.
9. A Lua compare-and-delete attempts to remove the owned lock.
10. Redis cleanup is best effort; MongoDB commit remains a successful booking
    even if cleanup fails.

## Seat State

The service starts with the showtime's ordered seat definitions, reads confirmed
bookings from MongoDB, and reads active locks with Redis `MGET`. Every configured
seat is returned. State precedence is:

1. `BOOKED`
2. `LOCKED`
3. `AVAILABLE`

## Redis Lock

```text
seat_lock:{showtimeId}:{seatNo}
{"user_id":"...","ownership_token":"..."}
```

The ownership token is 32 random bytes encoded as hexadecimal. Release compares
the complete serialized owner and deletes only inside a Lua script. A stale
token, including one for the same user, cannot remove a newer lock. Redis TTL
automatically makes abandoned locks available after five minutes. Confirmation
uses a separate compare-only Lua script. It returns missing, mismatched, or
matched atomically and never resets or increases the remaining TTL.

## Correctness Boundary

Redis and MongoDB cannot participate in one atomic transaction. MongoDB
insertion is the durable success boundary. If lock cleanup fails afterward, the
API still returns the confirmed booking and logs the cleanup failure without
ownership data. The lock expires at its original deadline. If a lock expires
during an in-flight confirmation, MongoDB's unique index still prevents two
confirmed bookings. The unique index, not the temporary lock, is the final
correctness boundary.

## Configuration

`MONGO_DATABASE` is required independently of connection addressing. A complete
`MONGO_URI` may be supplied, or the URI may be assembled from host and
credentials, but the database selected by the application is always the
explicit `MONGO_DATABASE` value. The application does not infer it from a URI.

## Deferred Work

- WebSocket event envelope and connection hub
- Redis Pub/Sub producers and consumers
- Audit and notification processing
- Firebase claim verification and role mapping
- Admin APIs and UI
- Reconciliation for rare post-commit lock cleanup failures
