# Implementation Plan

## Phase 1 - Scaffold and infrastructure

- [x] Create Go + Gin backend
- [x] Create Vue 3 + TypeScript + Vite frontend
- [x] Add MongoDB and Redis connections
- [x] Add health endpoint
- [x] Add Dockerfiles and Docker Compose
- [x] Add `.env.example`

## Phase 2 - Core domain and booking correctness

- [x] Seed movie, showtime, and seat data
- [x] Implement seat map API
- [x] Implement Redis seat lock with five-minute TTL
- [x] Add cryptographic lock ownership token
- [x] Implement ownership-safe lock release
- [x] Implement booking confirmation
- [x] Add MongoDB unique index for `(showtime_id, seat_no)`
- [x] Add focused unit and concurrency tests
- [x] Add opt-in real MongoDB/Redis integration coverage
- [x] Verify confirmation ownership atomically without renewing lock TTL
- [x] Treat MongoDB insertion as the durable booking success boundary
- [x] Make post-commit Redis cleanup best effort and observable
- [x] Require explicit `MONGO_DATABASE` configuration
- [x] Add focused HTTP error-contract coverage
- [x] Add real Redis ownership, stale-token, expiry, and TTL coverage

Phase 2 uses temporary `X-User-ID` and `X-User-Role` headers. Phase 4 replaces
this development-only boundary with verified Firebase claims.

## Phase 3 - Realtime and asynchronous events

- [x] Add versioned domain event contract
- [x] Publish seat and booking events through Redis Pub/Sub
- [x] Add WebSocket hub and showtime rooms
- [x] Broadcast public seat-state updates
- [x] Add asynchronous idempotent audit logging
- [x] Add Redis seat-lock expiration events
- [x] Add graceful background-worker lifecycle
- [x] Gate expiration events on durable booking state
- [x] Atomically suppress stale expiration events when a newer lock exists
- [x] Wait for Redis subscriber readiness before starting HTTP
- [x] Add per-seat Redis generations and generation-bearing expiry markers
- [x] Atomically combine public state transitions with event publication
- [x] Make `BOOKED` terminal for Redis realtime state
- [x] Exit non-zero after cleanup on startup failure
- [x] Add focused unit, race, and integration tests

## Phase 4 - Authentication and authorization

- [x] Add Firebase Authentication foundation
- [x] Verify Firebase ID tokens in backend
- [x] Add explicit development authentication mode
- [x] Add `USER` and `ADMIN` roles
- [x] Protect booking and admin APIs
- [x] Add filtered admin bookings endpoint
- [x] Add minimal frontend Firebase and authenticated API foundation
- [x] Add auth, role, admin, regression, and configuration tests

## Phase 5 - Frontend MVP

- [x] Login screen
- [x] Seat map
- [x] Lock countdown
- [x] Mock payment confirmation
- [x] Personal bookings list
- [x] Realtime seat updates and reconnect refresh
- [x] Admin booking table with exact user filter

## Phase 6 - Submission quality

- [ ] Review concurrency behavior
- [x] Complete final README and architecture diagram
- [x] Document final assumptions and trade-offs
- [ ] Record demo video
