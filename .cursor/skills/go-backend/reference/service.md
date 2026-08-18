# Service

Business logic and orchestration.

## Own

- Use cases, domain rules, authorization decisions.
- Transaction boundaries (begin/commit/rollback).
- Idempotency key ownership for money and other once-effects.
- Outbox publication for side effects that must follow a committed write.
- Domain errors (typed), wrapped with `%w` at boundaries.

## Do not

- Import gin or bind HTTP types.
- Run raw SQL / GORM queries (repositories do).
- Publish to NATS/Kafka inside a DB transaction without outbox.
- Log an error and continue instead of returning it (logging **and** returning is fine).

## Money paths

- CMS does not own ledger/wallet. Do not add balance mutations here.

## Checklist

- [ ] Transaction scope is obvious and minimal
- [ ] Side effects after commit are outbox-safe
- [ ] Concurrent access considered (locks / optimistic version)
