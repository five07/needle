---
id: TASK-002
title: Project scaffold, tooling, and README
status: To Do
assignee: []
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T04:28:52Z'
labels:
  - infra
milestone: m-0
dependencies: []
priority: high
type: chore
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
New repo with no code. Everything else needs a module, entrypoint, and agreed commands. Decided in PLAN.md: MIT license, module github.com/five07/tidal-tui (Q15), Linux only for v1 (Q4), adapted Definition of Done (Q14).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 go.mod declares module github.com/five07/tidal-tui and the current Go version
- [ ] #2 `go build ./cmd/tidal-tui` produces a binary that prints version with --version
- [ ] #3 README documents build, run, test, vet, and gofmt commands, and states Linux-only support and the mpv runtime requirement
- [ ] #4 LICENSE file contains the MIT license
- [ ] #5 CI workflow on Linux runs go test -race, go vet, and gofmt -l on push and PR
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Every acceptance criterion has at least one test that fails without the change
- [ ] #2 go test -race ./..., go vet ./..., and gofmt -l . are clean
- [ ] #3 Coverage: changed non-UI packages 95%+, TUI packages 80%+ (teatest/golden); API code has httptest coverage against a fake TIDAL server
- [ ] #4 Security self-review done (input validation, no token or internal leaks in errors, shared-state concurrency, no fail-open paths)
- [ ] #5 go build ./cmd/tidal-tui succeeds and the binary starts
- [ ] #6 README updated for behavior, config, keybinding, or tradeoff changes
- [ ] #7 Only dependencies named in the ticket added; no out-of-scope changes
- [ ] #8 slog logging to $XDG_STATE_HOME/tidal-tui/ with track_id/request_id fields; no tokens logged
<!-- DOD:END -->
