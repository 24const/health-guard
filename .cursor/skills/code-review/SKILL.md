---
name: code-review
description: Review already-written Go backend code (PR, branch, commit, or diff). Bug pass first (write-then-X, batch sibling, wrap/sentinel, 4xx-after-write), then caller-closure, spec table, contract fork A/B, sibling via gh pr view (no clone). Hunt as file:L one-liners on Critical/Required/Optional/Nit/FYI. Missing tests FYI unless money/migration or asked. User constraints (skip go.mod) stay FYI not Critical. Use when the user asks to review a PR, branch, diff, or already-written Go code. For Go backend PRs this skill wins over caveman-review (caveman only compresses hunt lines). Not for implementing or fixing.
---

# Go backend code review

Machine-checkable issues (formatting, unused imports, obvious vet findings) are gofmt / go vet / staticcheck / golangci-lint. Do not spend review budget on them. Review what tools cannot see: does the change do what the ticket asked, and is the Go correct under concurrency, errors, resources, data, and contracts.

## Boundaries

- Review only. Do not edit code, do not push, do not approve or request changes on the PR.
- Output findings ready to paste. Fixing them is a separate task under `.cursor/rules/fast-path.mdc`, only if the user asks.
- **Hunt first, report second.** Hunt is `file:L: severity: problem. fix.` on the one scale below. The report **contains** that list (see Report shape). Prose is only for a contract fork or why a hunt line is not a lone bug. A write-up without file:line bugs is incomplete. If asked to “write md”, the md includes the hunt list, not an essay instead of it.
- **Caveman-review** may compress hunt lines. It must not replace this report shape or swap in 🔴/🟡/🔵 as the report scale.
- **User constraint wins dependency-block.** If the query says skip a surface (`go.mod`, a sibling repo), that surface is FYI «вне скоупа», not Critical. Deploy order still belongs in Sibling.

## Do not

- Do not add an always-on rule “find bugs” — it fires on every chat and burns tokens.
- Do not spawn Task/subagents for every PR: `token-discipline.mdc` forbids it, and a second agent without the caller-closure exception repeats the same hole.
- Do not copy this review checklist into `go-backend/SKILL.md` — that skill is for implementation and is loaded every session.
- Do not clone sibling repos. Do not hunt bugs in their diffs as if they were this PR. Sibling is `gh pr view` after this-module bugs (protocol below).

## Procedure

1. **Get the change** — from a PR (`gh pr diff <n>`, `gh pr view <n>`), a branch (`git diff <base>...HEAD`), staged work (`git diff --staged`), or the paths the user passed. Ambiguous which one? Ask. Read the diff plus the files it touches — no wider tour (`.cursor/rules/token-discipline.mdc`), except **Caller closure** below.
2. **Load domain** — `go-backend` is already required every session. Additionally: diff touches `migrations/` or `sql/` → load `.cursor/skills/migration-review/SKILL.md` + `.cursor/rules/migrations.mdc`.
3. **Understand first** — read the ticket and trace the real flow through the code it touches (handler → service → repo). Do not comment line by line before you understand the change. `*_test.go` inside the change is in scope.
3b. **Bug pass** — mandatory, even if spec is already red. A structural hole leads the report; it does not skip this step. Then **Caller closure**. Then spec. Then **contract fork**. Then standards. Then sibling. Then report.
4. **Spec axis** — table against the originating ticket (below). Then contract fork if the diff left the plan on purpose.
5. **Standards axis** — walk the Go checks (below). Point at existing rules; do not re-derive them.
6. **Sibling / deploy** — `gh pr view` / `gh pr list` for PRs named in the ticket (core, cms-ai, gateway). After this-module bugs, not instead of them. Protocol below. Do not clone.
7. Report in the **Report shape** skeleton. Hunt one-liners stay in the report as file:line findings.

## 3b. Bug pass

A structural hole leads the report. It does not skip this step. Drop nits. Do not drop a bug in a function you already opened (unread `if err` branch stays).

For every **new or changed** function in the diff, answer on paper (not in the report — for yourself). No answer = finding or `q:`:

1. **Write-then-X:** after a DB write, the next call fails — what does the client see vs what is persisted?
2. **Batch sibling:** one bad item — do valid items still get sent? Retry of the same request: success / no-op / fail forever?
3. **Wrap:** `fmt.Errorf("%w", other)` on a path where the handler does `errors.Is` / `GamesEnablingHTTPResponse` (or equivalent typed mapper) — is the sentinel still alive?
4. **HTTP status vs side effect:** 4xx after a successful write is a lying contract.
5. **`_ = err` / `go f()` / log-and-continue** on a path that was just made synchronous so the operator sees the error.

Then spec. Then contract fork. Then standards. Then sibling. Then report.

## Caller closure (not a tour)

If the diff changes a signature, error type, or deletes a method — Grep **that name** in the same Go module (`internal/`, `cmd/`).

Goal: handlers/wrappers not in the diff that do not map the new error or still call the old name.

Forbidden: sibling services, migration history, “what else might exist.” One symbol → its call sites. Found something unrelated — do not follow it.

Grep is allowed only because `token-discipline.mdc` has a review exception (`code-review` skill only): symbols the diff adds, renames, or whose error/return type changed; same Go module only. Without that exception, by-provider / leftover callers stay out of scope.

## Spec axis — did it build the right thing

Find the originating spec: issue references in commit messages (`#123`, `Closes BF-1550`), a path the user passed, or ask. If there is none, note "no spec available" and skip this axis.

When a plan/ticket exists, report a table: requirement → in the diff (`Да` / `Нет` / `Частично`). Cover at least: missing or partial asks, scope creep, looks-implemented-but-wrong.

Code can follow every Go standard and still implement the wrong thing — this axis catches that.

**Contract fork:** if the diff deliberately left the plan (new error text, no compensate, fire-and-forget kept), do not file that as a lone bug. State two coherent fixes:

- **A** — restore the plan (and who else must match).
- **B** — keep the new contract (and which siblings must match: toast, HTTP `code`, retry).

A mix (CMS saved + UI rolls back / 4xx after write) is Required until one is chosen. Do not invent a sibling toast from the plan — read that PR or write `не проверено`.

## Standards axis

Apply these rules instead of restating them:

| Topic | Rule |
|-------|------|
| Carve-out zones / YAGNI / root cause | `.cursor/rules/fast-path.mdc` |
| Handler / DTO / service / repo boundaries | `.cursor/rules/layering.mdc` |
| HTTP + event payload shapes, compatibility | `.cursor/rules/api-contract.mdc` |
| Consumers, ack vs side effect, outbox, DLQ | `.cursor/rules/messaging.mdc` |
| PII in logs, zap fields, `%w`, `context.Background()` | `.cursor/rules/observability-pii.mdc` |
| SQL migrations | `.cursor/rules/migrations.mdc` + migration-review skill |
| What a good test looks like | `.cursor/rules/test-review.mdc` |
| What a test claim is worth | `.cursor/rules/claims-and-verification.mdc` |

Fast-path taxonomy never skips this review. If the change touches a carve-out zone listed in `.cursor/rules/fast-path.mdc`, run the full checklist however small the diff.

### Concurrency (highest value, hardest to see)

- Shared state mutated without synchronization (mutex, atomic, channel). Data race.
- Goroutines with no clear exit — goroutine leak. Every goroutine needs a way to stop.
- Background work that ignores `context` cancellation.
- Do **not** flag loop-variable capture (Go 1.22+ is per-iteration). Flag sharing a variable declared *outside* the loop, or `go f()` with no wait / errgroup.
- Channels that can block forever, or a send on a possibly-closed channel.

### Errors

- Logging an error and continuing, or `_ =` where the error matters. Logging **and** returning the same error is an accepted pattern — do not flag it as double handling.
- Wrapping that drops context — `fmt.Errorf` without `%w` when a caller does `errors.Is` / `errors.As`.
- **Wrap substitution:** rollback/`Remove` wrapped as `fmt.Errorf("%w", removeErr)` drops the sentinel the handler maps.
- **Fail-all:** early return on one bad batch item that also lists (or skips) valid IDs — those valids never send; same-request retry can fail forever.
- **4xx after write:** HTTP 400/404 after a committed CMS/DB write. Client treats it as rejected; data is not.
- **Typed error in one handler:** a sibling handler calls the same `svc.Foo` and still returns generic 500.
- `panic` where a returned error belongs.

### Resources and leaks

- Missing `defer` close on `rows`, `resp.Body`, files.
- `context.WithCancel` / `WithTimeout` whose `cancel` is never called.
- Unbounded growth in slices or maps on the request path.

### Data integrity (gorm + Postgres)

- **Transactions**: money / balance / multi-row writes run inside `db.Transaction(func(tx *gorm.DB) error {...})` or an explicit `Begin`, not separate calls that can half-apply. Check the whole unit of work is one transaction.
- **gorm zero-value skip**: `Updates` with a struct omits zero-value fields — a real `0` / `false` / `""` won't be written. Flag struct-based `Updates` where zero is valid (amount, count, flags); require `Select` or a `map[string]interface{}`.
- **Missing WHERE**: flag `Session{AllowGlobalUpdate: true}` or raw exec that could hit all rows.
- **N+1**: a query per item instead of `Preload` / `Joins` / a batched `IN`. Required (not Nit) on hot paths.
- **Locking**: read-modify-write on a balance without `clause.Locking{Strength: "UPDATE"}` or an atomic update — concurrent requests race and lose writes.
- **Raw SQL**: string-concatenated queries instead of parameters — SQL injection. Critical.
- **Currency**: money in floats, or inconsistent rounding.
- **Idempotency**: retryable HTTP (payment, webhook) without an idempotency key or uniqueness guard — double debit / credit. Consumer-side dedupe, publish-inside-transaction and ack semantics → `.cursor/rules/messaging.mdc`.

### Tests

What a good test looks like is `.cursor/rules/test-review.mdc` — apply it, do not re-derive it. At review time hunt for:

- **The strongest single check** — mentally introduce a behaviour-changing bug and ask whether any test would fail. If the feature can be broken while every test stays green, the suite does not test behaviour. This one check catches tautologies, implementation-shaped tests and coverage gaps at once.
- Tautological tests: a mock is set to return X and the test asserts X came back. Ask which real bug would turn it red; no answer means the test is useless.
- Tests that would break on a behaviour-preserving refactor (renaming an unexported method, reordering internal steps) — coupled to implementation.
- Happy path only: no empty or invalid input, no limits exceeded, no dependency failure, no concurrent access.
- Money / payment / commission: every invariant asserted as an observable outcome, never as a call. "After two identical requests the amount is debited once", not "the handler was called with these arguments". Implementation-shaped tests are most dangerous here, because they stay green while money is lost.
- Scope: review the tests that are in the change. Demand new test files only in a `fast-path.mdc` carve-out zone or on an explicit ask — `.cursor/rules/token-discipline.mdc` keeps tests opt-in.
- **Missing tests in this repo (tests opt-in): FYI or Optional.** Required only if the user asked to verify, or the change is money/migration. Do not spend a Required slot on “no `*_test.go`” while the bug pass still has unread `if err` branches.
- Coverage owed per change type, and what may be claimed from it, is in `.cursor/rules/claims-and-verification.mdc`. When tests *are* in the change, a missing behaviour check there is a finding.

## Dependency changes

- New dependency: does the existing stack solve it first? Every dependency is a liability.
- Upgrading an existing one is a code change: read the changelog, not just the version bump (semver can hide behaviour changes); one dependency per change so a break is traceable; review the `go.sum` / lockfile diff; let a green test suite decide, and if coverage around it is thin, that gap is the finding.
- User said skip `go.mod` / a bump is intentional → FYI «вне скоупа», not a merge blocker. Note deploy: tag/bump still required before this PR compiles.

## Change sizing

- ~100 lines: good. ~300: acceptable if one logical change. ~1000: too large, ask to split.
- Watch file size, not just diff size. A small diff can push a file past ~1000 total lines — extract helpers or modules first, then add.
- Separate refactoring from feature work — submit them as two changes.

## Sibling / deploy (after this-module bugs)

Do not clone. Do not review sibling diffs as this PR.

If the ticket, plan, or user names core, cms-ai, st8, prism-gateway (or another producer/consumer):

1. `gh pr list --search <ticket id>` / `gh pr view <n> --repo <org/repo>`.
2. UI sibling: read the **actual** toast/rollback in that PR, not the plan copy.
3. This PR produces to a new subject and the consumer PR is missing → **Required** on deploy of *this* PR (“не мержить CMS до gateway X”), not Critical “this CMS code is wrong”.
4. Sibling not opened → `не проверено: <name>`. Do not invent their message or handler.

## Report shape

Fixed skeleton. Do not omit 1–5. Do not replace the hunt with an essay.

1. **Verdict** — one line (merge / не мержить / нужны правки) + size if known.
2. **Hunt** — fenced list, every finding `file:L: severity: problem. fix.` Caveman may compress these lines only.
3. **Critical / Required** — extra paragraph only when a hunt line needs A/B or why. Do not restate hunt lines as prose.
4. **Spec** — table requirement → in the diff (`Да` / `Нет` / `Частично`). Then contract fork if the diff left the plan.
5. **Sibling / deploy** — facts from `gh`, or `не проверено: <repo>`.
6. **Optional / Nit / FYI** — remaining hunt lines or short bullets. No essay.

Per finding: severity, file:line, what breaks, named fix.

One severity scale, used everywhere in the report:

- **Critical** — blocks merge: security hole, data loss, broken functionality (happy path broken *now*).
- **Required** — must change before merge (error-path hole, lying HTTP, deploy-order).
- **Optional** — worth doing, not required.
- **Nit** — style preference, author may ignore.
- **FYI** — context only, no action.

Translate into this scale; do not mix scales in one report:

| Incoming | This report |
|---|---|
| migration-review `P0` | Critical |
| migration-review `P1` | Required |
| migration-review `note` | Optional or FYI |
| caveman `🔴 bug` | Critical if broken on the happy path now, else Required |
| caveman `🟡 risk` | Required or Optional |
| caveman `🔵 nit` | Nit |
| caveman `❓ q` | FYI, or drop if unanswered |

Don't rubber-stamp and don't soften real issues. Quantify when you can ("this N+1 adds ~50ms per list item" beats "this could be slow"). If the author has full context and overrides, defer gracefully.
