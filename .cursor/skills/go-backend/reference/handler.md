# Handler / controller

Transport only.

## Own

- Route registration, gin binding and validation.
- Extract authentication / brand / request context.
- Map DTO ↔ service inputs/outputs at the edge.
- Map domain errors to stable HTTP codes and error envelopes in **one** place.
- Propagate `context.Context` from the request.

## Do not

- Business rules, authorization policy decisions beyond “is authenticated / has role X if already established”.
- Start transactions (services own them).
- Import repository packages or GORM models.
- Log request bodies on auth / cashier endpoints.
- Call `context.Background()`.

## Checklist

- [ ] Validation at the boundary
- [ ] Typed error → HTTP mapping reused, not reinvented
- [ ] No SQL / GORM in this file
- [ ] Contract/OpenAPI updated if the public shape changed
