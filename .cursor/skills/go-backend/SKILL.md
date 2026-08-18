---
name: go-backend
description: Write and modify Go backend code as a senior Golang engineer. Use for backend features, refactors, bug fixes, SQL/migration updates, messaging consumers, and architectural changes in Go services.
---

# Go Backend Skill

Stack: Go 1.25, gin, nats, kafka, gorm, zap.

Paths below are from the Go service repo root (workspace), not from this file.

## Setup (every session)

1. Read root [`AGENTS.md`](AGENTS.md) if not already in context.
2. For the layer you touch, read **one** file under `reference/` (not all):
   - [`.cursor/skills/go-backend/reference/handler.md`](.cursor/skills/go-backend/reference/handler.md) — transport / gin
   - [`.cursor/skills/go-backend/reference/dto.md`](.cursor/skills/go-backend/reference/dto.md) — contracts / mapping
   - [`.cursor/skills/go-backend/reference/service.md`](.cursor/skills/go-backend/reference/service.md) — business logic / transactions
   - [`.cursor/skills/go-backend/reference/repository.md`](.cursor/skills/go-backend/reference/repository.md) — persistence / GORM
3. Prefer live code and Cursor rules over rediscovering contracts from scattered docs:
   - HTTP: [`internal/handler/routes.go`](internal/handler/routes.go) + `.cursor/rules/api-contract.mdc`
   - Events / RPC: `.cursor/rules/messaging.mdc` + `internal/rpc`
   - Migrations: `.cursor/rules/migrations.mdc`
   - Logs / PII: `.cursor/rules/observability-pii.mdc`
   - Verification claims: `.cursor/rules/claims-and-verification.mdc`
4. Migrations → also load [`.cursor/skills/migration-review/SKILL.md`](.cursor/skills/migration-review/SKILL.md).

## Cursor rules (do not duplicate here)

| Topic | Rule |
|-------|------|
| Layering | `.cursor/rules/layering.mdc` |
| Errors / PII / logging | `.cursor/rules/observability-pii.mdc` |
| Scope / minimal diff / no tests unless asked | `.cursor/rules/token-discipline.mdc`, `fast-path.mdc` |
| Verification claims | `.cursor/rules/claims-and-verification.mdc` |
| HTTP / event contracts | `.cursor/rules/api-contract.mdc` |
| Messaging | `.cursor/rules/messaging.mdc` |
| SQL migrations | `.cursor/rules/migrations.mdc` |
