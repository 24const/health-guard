---
name: migration-review
description: Review a SQL migration for expand/contract safety, backfill policy, index strategy, and rollback/mitigation. Use when adding or changing files under migrations/ or sql/.
---

# Migration review

Read `.cursor/rules/migrations.mdc` first.

## Procedure

1. **Intent** — what schema/data change and which deploy it ships with.
2. **Expand/contract** — is a rename/drop split across two releases? If not, flag.
3. **Locking** — table rewrites, long transactions, missing `CONCURRENTLY` on large indexes.
4. **Backfill** — in-migration unbounded `UPDATE`? Should be a job.
5. **Down / irreversible** — tested down exists, or explicit irreversible + mitigation.
6. **App compatibility** — old binaries still work against the new schema during rollout.
7. **Validation** — scratch DB plan (up or up/down/up).

## Report shape

- Risk level (P0 blocking / P1 before prod / note)
- Checklist results
- Suggested split into expand → migrate traffic → contract if needed
- Verification commands from `.cursor/rules/claims-and-verification.mdc`
