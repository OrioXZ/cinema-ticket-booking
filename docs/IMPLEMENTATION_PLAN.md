# Implementation Plan

## Phase 1 — Scaffold and infrastructure

- Create Go + Gin backend
- Create Vue 3 + TypeScript + Vite frontend
- Add MongoDB and Redis connections
- Add health endpoint
- Add Dockerfiles
- Add root `docker-compose.yml`
- Add `.env.example`
- Confirm `docker compose up --build`

## Phase 2 — Core domain and booking correctness

- Seed movie, showtime, and seat data
- Implement seat map API
- Implement Redis seat lock with five-minute TTL
- Add lock ownership token
- Implement booking confirmation
- Add MongoDB unique index for `(showtime_id, seat_no)`
- Add focused concurrency tests

## Phase 3 — Realtime and asynchronous events

- Add WebSocket hub
- Publish seat and booking events through Redis Pub/Sub
- Broadcast updates to clients
- Add asynchronous audit logging

## Phase 4 — Authentication and authorization

- Add Firebase Authentication
- Verify Firebase ID tokens in backend
- Add `USER` and `ADMIN` roles
- Protect admin APIs

## Phase 5 — Frontend MVP

- Login screen
- Seat map
- Lock countdown
- Mock payment confirmation
- Admin booking table
- One useful filter

## Phase 6 — Submission quality

- Validate Docker startup
- Review concurrency behavior
- Complete README
- Add architecture diagram
- Document assumptions and trade-offs
- Record demo video
