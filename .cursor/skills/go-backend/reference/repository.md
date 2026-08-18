# Repository

Persistence and data access only.

## Own

- GORM / SQL queries with explicit column lists where practical.
- Tenant / brand scoping on every query that needs it.
- Locking / optimistic concurrency helpers used by services.
- Mapping between storage models and domain entities if that is the project pattern.

## Do not

- Encode business policy (beyond “row not found” / constraint translation).
- Silently convert missing rows into zero values for money or identity.
- Expose unbounded `Find` without pagination when tables can grow.
- Own HTTP or messaging publish.

## Checklist

- [ ] Deterministic queries; indexes assumed or migration planned
- [ ] Errors wrapped with query intent
- [ ] No float money types
