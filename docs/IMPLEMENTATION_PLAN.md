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
- [x] Add focused unit, race, and integration tests

## Phase 4 - Authentication and authorization

- [ ] Add Firebase Authentication
- [ ] Verify Firebase ID tokens in backend
- [ ] Add `USER` and `ADMIN` roles
- [ ] Protect admin APIs

## Phase 5 - Frontend MVP

- [ ] Login screen
- [ ] Seat map
- [ ] Lock countdown
- [ ] Mock payment confirmation
- [ ] Admin booking table with one useful filter

## Phase 6 - Submission quality

- [ ] Review concurrency behavior
- [ ] Complete final README and architecture diagram
- [ ] Document final assumptions and trade-offs
- [ ] Record demo video
