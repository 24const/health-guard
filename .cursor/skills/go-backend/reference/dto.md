# DTO

Request/response contracts and mapping boundaries.

## Own

- Wire shapes for HTTP (and event payloads when they are DTO-shaped at the edge).
- Validation tags / custom validators consistent with the repo.
- Mapping to/from domain types at the handler or dedicated mapper — not inside repositories.

## Do not

- Become the persistent model. Do not pass DTOs through services into GORM.
- Hand-maintain fields that are generated from OpenAPI/protobuf if that is the repo’s source of truth direction.
- Embed secrets or dump full structs into logs.

## Checklist

- [ ] Public change has contract/schema update or regenerate-and-diff plan
- [ ] Backwards compatibility considered (additive preferred)
- [ ] Mapping is explicit; no silent zero-value surprises for money/IDs
