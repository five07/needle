# CLAUDE.md

Standing instructions for every change in this repo. Per-change work is defined in `PLAN.md`. If the two conflict, stop and ask.

---

## Project (project)


### Layout

Refer to the actual project layout

### Commands

Refer to commands in the `README.md`

---

## Invariants (project)

These must hold after every change. A change that requires breaking one is out of scope until I approve it.


---

## Git

- **Never** run `git commit`, `git push`, `git merge`, `git rebase`, `git tag`, `git reset`, `git stash`, `git clean`, `git checkout`/`git switch`, `git restore`, or any command that creates, rewrites, or discards history or working-tree changes.
- Read-only git commands are allowed: `git status`, `git diff`, `git log`, `git show`, `git blame`.
- I review the diff and commit myself. Do not suggest commit messages unless asked.

---

## Workflow

1. **Read** `PLAN.md`, this file, and the README sections relevant to the change.
2. **Questions + plan (stop).** Append questions to the QUESTIONS section of `PLAN.md` following the rules below. In the same stop, post an brief summarized implementation plan in chat: the approach, the files to touch, and a breakdown into discrete tickets. Then stop and wait for approval.
3. **Implement** one ticket at a time, in the approved order. Write tests alongside the code, not after. Do not work on sections of the codebase that are not directly related to the ticket.
4. **Verify** against the Definition of Done before reporting.
5. **Report** using the format below, then stop.

Do not begin implementation until I approve the plan. If I approve with changes, restate the changes in one line before starting.

---

## Questions rules

1. Ask all questions at once, before any code, numbered. Reserve a nested annotation for the response, marked by `> MS: `
2. Only ask when the answer changes behavior, the API, or tests. Otherwise decide, and record it under Assumptions in the report.
3. Every question includes options and a recommended default.
4. Never ask anything already answered in `PLAN.md`, this file, or the README.
5. If a new ambiguity appears mid-implementation and would change a requirement, stop and ask. Otherwise, log it and continue.

---

## Definition of Done (standard)

Applies to every change. `PLAN.md` may add to it or explicitly override an item. Do not update the DoD here, copy them to the `PLAN.md` and mark as done there. These items are a template only!

- [ ] Every requirement in `PLAN.md` is met and has at least one test that fails without the change.
- [ ] `go test -race ./...`, `go vet ./...`, and `gofmt -l .` are clean.
- [ ] Changed packages are at 95%+ statement coverage. API changes have `httptest` integration coverage against the real handlers, which serves as the e2e layer for this service.
- [ ] Security self-review of the diff covers input validation, error messages that don't leak internals, concurrency on shared state, and no new fail-open paths. Findings go in the report.
- [ ] `go build ./cmd/server` succeeds and the server starts.
- [ ] The README is updated wherever behavior, the API, or tradeoffs changed.
- [ ] No new dependencies, no changes outside the scope in `PLAN.md`, and all invariants hold.

---

## Code and docs style

- Idiomatic Go: wrap errors with `%w`, use sentinel or typed errors for anything callers branch on, return errors instead of panicking, and pass `context.Context` as the first parameter of any I/O.
- Use table-driven tests, with stubs that assert call counts where behavior depends on what was (or wasn't) called.
- Logging uses `log/slog` with the order ID as a structured field.
- Doc comments go on every exported identifier.
- Prose in docs and comments uses American English and never uses em dashes. Use commas, colons, or restructure.

---

## Report format

```
## Summary
One paragraph: what changed and why.

## Tickets
- [x] T1: ...
- [x] T2: ...

## Files touched
path: one-line description

## Verification
Test, vet, and gofmt output (abbreviated). Coverage per changed package.

## Security review
Findings, or "none" with what was checked.

## Assumptions
Decisions made without asking, per Questions rule 2.

## Deviations
Anything that differs from the approved plan, and why.

## Follow-ups
Out-of-scope issues noticed but not acted on.
```

<!-- BACKLOG.MD GUIDELINES START -->
<!-- backlog.md-instructions-version: 1.57.2 -->
<CRITICAL_INSTRUCTION>

## Backlog.md Workflow

This project uses Backlog.md for task and project management.

**At the beginning of each conversation in this project, run `backlog instructions overview` before answering or taking action. Re-read it only if you have not read it yet in the current conversation.**

Use the overview to decide whether to search, read, create, or update Backlog tasks.

Before task lifecycle actions, read the matching detailed guide:
- `backlog instructions task-creation` before creating or splitting tasks
- `backlog instructions task-execution` before planning, changing status or assignee, adding a plan or implementation notes, or implementing task work
- `backlog instructions task-finalization` before checking acceptance criteria, writing final summaries, or moving tasks to terminal statuses

Use `backlog <command> --help` before running unfamiliar commands. Help shows options, fields, and examples.

Do not edit Backlog task, draft, document, decision, or milestone markdown files directly. Use the `backlog` CLI so metadata, relationships, and history stay consistent.

</CRITICAL_INSTRUCTION>
<!-- BACKLOG.MD GUIDELINES END -->
