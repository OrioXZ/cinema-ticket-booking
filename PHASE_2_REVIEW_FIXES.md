Read `AGENTS.md`, `README.md`, and `docs/ARCHITECTURE.md` before making changes.

Work only on the current `phase-2-core-booking` branch.

Apply a focused Phase 2 correctness cleanup based on code review. Do not redesign the architecture and do not implement any Phase 3 or later functionality.

## 1. Do not renew a valid seat lock for another full five minutes during confirmation

Current confirmation calls `VerifyAndExtend` and resets the lock TTL to the full five-minute duration.

Change this behavior so repeated confirmation attempts cannot indefinitely extend a seat lock beyond the original booking window.

Preferred solution:

* Replace `VerifyAndExtend` with an atomic ownership verification operation that does not change the TTL.
* The Lua script must atomically:

  * return missing when the lock does not exist,
  * return not-owned when the complete serialized owner value does not match,
  * return matched when ownership matches,
  * never change the key TTL.

Do not use a separate Redis `GET` followed by application-side comparison.

Rename interfaces, implementations, tests, and documentation so they describe verification rather than extension.

After the change:

* A correct user and ownership token may confirm only while the original lock is still active.
* Repeated confirmation requests must not refresh the five-minute expiry.
* An incorrect user or token must not change the TTL.
* An expired lock must remain expired.

## 2. Treat MongoDB commit as the booking success boundary

Current confirmation inserts the durable booking and then returns an error if Redis lock cleanup fails.

Change the behavior so that once MongoDB successfully creates the booking:

* The API returns the confirmed booking successfully.
* Redis lock release is best effort.
* A Redis cleanup error must not turn a committed booking into HTTP 500.
* The cleanup failure should be logged without exposing ownership tokens or credentials.
* The remaining Redis lock may expire naturally through its TTL.
* Do not delete or roll back the MongoDB booking.

Keep duplicate booking behavior unchanged:

* MongoDB duplicate-key remains an HTTP 409 seat conflict.
* The unique `(showtime_id, seat_no)` index remains the final double-booking barrier.

Use an injected logger or another small testable mechanism rather than tightly coupling the service to global logging if practical. Keep the change minimal.

## 3. Make MongoDB database configuration unambiguous

Currently `MONGO_URI` may reference one database while `MONGO_DATABASE` silently defaults to `cinema`.

Make `MONGO_DATABASE` explicitly required whenever the application starts, including when `MONGO_URI` is supplied.

Requirements:

* Remove the silent `cinema` default for `MongoDatabase`.
* Return a clear configuration error when `MONGO_DATABASE` is absent.
* Keep supporting:

  * a complete `MONGO_URI`, or
  * `MONGO_HOST`, `MONGO_USERNAME`, `MONGO_PASSWORD`, and `MONGO_DATABASE`.
* Keep `.env.example`, Compose configuration, README, tests, and integration-test instructions synchronized.
* Do not add database-name parsing from `MONGO_URI`.

## 4. Add real Redis ownership and TTL integration coverage

Expand the existing integration test using the real local Redis service.

Cover at least:

1. A wrong ownership token cannot release a lock.
2. A stale token belonging to the same user cannot release a newer lock.
3. A wrong user or token cannot verify ownership.
4. Ownership verification does not increase or reset the remaining TTL.
5. An expired lock can be acquired again.
6. A valid matching owner can release the lock.

Avoid timing assertions that are unnecessarily fragile. Use a short test-only TTL and reasonable tolerance.

Ensure test cleanup removes all known Redis keys even when an assertion fails.

## 5. Add HTTP handler tests

Add focused handler tests for the API error contract:

* Missing `X-User-ID` returns HTTP 401 with `IDENTITY_REQUIRED`.
* Invalid seat returns HTTP 400 with `INVALID_SEAT`.
* Unknown showtime returns HTTP 404 with `SHOWTIME_NOT_FOUND`.
* Already locked or booked seat returns HTTP 409 with `SEAT_CONFLICT`.
* Missing or expired lock returns HTTP 409 with `LOCK_NOT_ACTIVE`.
* Wrong user or ownership token returns HTTP 403 with `LOCK_NOT_OWNED`.
* Successful lock acquisition returns HTTP 201.
* Successful confirmation returns HTTP 201.
* Successful release returns HTTP 204.

Keep tests deterministic and use fakes where real infrastructure is unnecessary.

## 6. Add service regression tests

Add tests demonstrating that:

* Verification does not extend the original lock expiry.
* Repeated failed confirmation attempts do not extend the lock.
* A booking is returned successfully when MongoDB insert succeeds but Redis cleanup returns an error.
* The durable booking still exists in that cleanup-failure case.
* A duplicate MongoDB booking remains a seat conflict.
* No raw Redis or MongoDB error details are returned by handlers.

## 7. Update documentation

Update:

* `README.md`
* `docs/ARCHITECTURE.md`
* `docs/IMPLEMENTATION_PLAN.md`
* `AGENTS.md` only if validation commands change

Documentation must state clearly:

* Confirmation verifies ownership atomically without resetting the original five-minute TTL.
* MongoDB insertion is the durable success boundary.
* Redis cleanup after commit is best effort.
* A post-commit cleanup failure can temporarily leave the seat lock until TTL expiry.
* The MongoDB unique index remains authoritative.
* `MONGO_DATABASE` is required even when `MONGO_URI` is used.

Do not claim that reconciliation, Pub/Sub, audit processing, or realtime updates exist yet.

## Scope exclusions

Do not add:

* Firebase Authentication
* WebSocket
* Redis Pub/Sub
* Audit consumers
* Notifications
* Admin APIs
* Booking frontend UI
* Distributed transactions
* New heavy dependencies

## Validation

Run and report:

* `gofmt -w cmd internal`
* `go mod tidy`
* `go test ./...`
* `go test -race ./internal/booking`
* Real MongoDB/Redis integration tests
* Go build
* Frontend type check
* Frontend production build
* `docker compose config --quiet`
* Full Compose build and startup
* Direct and proxied health endpoints
* Manual or scripted lock, conflict, confirmation, and release API checks
* `git diff --check`
* Compose shutdown

Before editing, briefly report:

1. The exact files you expect to change.
2. How ownership verification will work after removing TTL extension.
3. How the service will represent and test post-commit Redis cleanup failure.
4. Any compatibility impact on existing tests or API responses.

After implementation, report:

* Files changed
* Behavioral changes
* Tests added
* Exact validation results
* Remaining limitations
* Anything intentionally deferred
