# Cinema Ticket Booking Assignment

## Objective

Build a small full-stack cinema ticket booking system that demonstrates:

- Backend and frontend engineering
- Concurrency handling
- Distributed locking
- Realtime seat updates
- System design and DevOps
- Code quality and clear technical explanation

The system should remain correct when multiple users attempt to select the same seat concurrently.

## Required stack

- Go backend
- Vue 3 frontend
- MongoDB
- Redis distributed locking
- WebSocket or SSE for realtime communication
- Kafka, RabbitMQ, or Redis Pub/Sub for at least one real asynchronous use case
- Google OAuth 2.0 or Firebase Authentication
- Docker and Docker Compose

The complete application must run using:

```bash
docker compose up --build
```

## User features

### Authentication

- Users sign in through Firebase Authentication with Google.
- The backend receives a verified user identity.
- Bookings are associated with that user.

### Seat map

Display the seats for a showtime using these states:

- `AVAILABLE`
- `LOCKED`
- `BOOKED`

When a seat state changes, other users viewing the same showtime must see the update in realtime.

### Booking flow

1. The user selects an available seat.
2. The backend attempts to create a Redis distributed lock.
3. A successful lock lasts five minutes.
4. Other users cannot lock the same seat during that period.
5. A successful mock payment converts the seat into a confirmed booking.
6. An expired unpaid lock releases the seat.
7. The design must prevent double booking during concurrent requests.

## Admin features

- View all bookings.
- Filter bookings by at least one field, such as movie, date, status, or user.
- Restrict admin endpoints and UI to users with the admin role.

## Audit logs

Record important events including:

- Successful booking
- Booking timeout
- Seat release
- Lock acquisition failure
- Relevant system errors

Audit logging may be asynchronous.

## Message queue requirement

Redis Pub/Sub will be used as the project event transport.

It must support real behavior such as:

- Publishing seat-state changes
- Triggering WebSocket broadcasts
- Writing asynchronous audit logs
- Triggering a mock notification after booking success

It must not be included without an active consumer.

## Security and configuration

- Support `USER` and `ADMIN` roles.
- Normal users must not be able to access admin APIs.
- Secrets and environment-dependent values must not be hardcoded.
- Firebase configuration and admin email configuration must use environment variables.

## Deliverables

- Backend source code
- Frontend source code
- `docker-compose.yml`
- `.env.example`
- README containing:
  - Architecture diagram
  - Technology overview
  - Booking flow
  - Redis lock strategy
  - Message queue usage
  - Setup and run instructions
  - Assumptions and trade-offs

## Optional additions

Only after the core system is correct:

- Postman collection
- Focused automated tests
- Mock notification

## Evaluation priorities

The highest priorities are:

1. Architecture and concurrency design
2. Correct booking, locking, and realtime behavior
3. DevOps and security
4. Code quality and documentation

UI polish and a real payment gateway are not required.
