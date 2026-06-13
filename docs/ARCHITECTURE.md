# Architecture

Status: Draft

This document should be updated as implementation decisions become concrete.

## Planned components

- Vue 3 frontend
- Go + Gin backend
- MongoDB
- Redis distributed locks
- Redis Pub/Sub
- WebSocket connections
- Firebase Authentication
- Docker Compose

## Initial flow

1. User signs in through Firebase.
2. Frontend calls the backend with a Firebase ID token.
3. Backend verifies the token and resolves the application role.
4. User requests the seat map for a showtime.
5. User attempts to lock a seat.
6. Backend creates a five-minute Redis lock using an ownership token.
7. Backend publishes a seat-state event.
8. WebSocket subscribers receive the update.
9. User confirms mock payment.
10. Backend verifies lock ownership and creates the booking.
11. MongoDB unique indexing prevents duplicate bookings.
12. Backend publishes booking and audit events.

## Open decisions

- Final backend package layout
- Exact WebSocket event envelope
- Exact Redis key format
- Timeout handling strategy
- Audit log consumer implementation
