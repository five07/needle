---
id: TASK-002
title: Project scaffold, tooling, and README
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T04:55:57Z'
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
- [x] #1 go.mod declares module github.com/five07/tidal-tui and the current Go version
- [x] #2 `go build ./cmd/tidal-tui` produces a binary that prints version with --version
- [x] #3 README documents build, run, test, vet, and gofmt commands, and states Linux-only support and the mpv runtime requirement
- [x] #4 LICENSE file contains the MIT license
- [ ] #5 CI workflow on Linux runs go test -race, go vet, and gofmt -l on push and PR
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 Every acceptance criterion has at least one test that fails without the change
- [x] #2 go test -race ./..., go vet ./..., and gofmt -l . are clean
- [x] #3 Coverage: changed non-UI packages 95%+, TUI packages 80%+ (teatest/golden); API code has httptest coverage against a fake TIDAL server
- [x] #4 Security self-review done (input validation, no token or internal leaks in errors, shared-state concurrency, no fail-open paths)
- [x] #5 go build ./cmd/tidal-tui succeeds and the binary starts
- [x] #6 README updated for behavior, config, keybinding, or tradeoff changes
- [x] #7 Only dependencies named in the ticket added; no out-of-scope changes
- [ ] #8 slog logging to $XDG_STATE_HOME/tidal-tui/ with track_id/request_id fields; no tokens logged
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Keep the existing go.mod (module github.com/five07/tidal-tui, go 1.27).
2. Add cmd/tidal-tui/main.go: main delegates to a testable run(args, stdout, stderr) int. Supports --version (version string set via -ldflags "-X main.version=...", default "dev") and -h usage. The TUI itself comes in TASK-009, so a plain invocation prints usage for now.
3. Table-driven test for run covering --version, no args, -h, and an unknown flag.
4. README: what it is, current status (30 s previews), Linux-only, mpv runtime requirement, build/run/test/vet/gofmt commands, version stamping, and the spike tool.
5. LICENSE: MIT.
6. .github/workflows/ci.yml on ubuntu-latest: setup-go from go.mod, gofmt check, go vet, go test -race, go build of cmd/tidal-tui.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented 2026-11-21.
- cmd/tidal-tui: main delegates to run(args, stdout, stderr) int; --version prints "tidal-tui <version>" (version via -ldflags -X main.version, default dev); no args prints a not-implemented notice plus usage until TASK-009.
- Table-driven tests (6 cases); coverage 95.5% (only the one-line main wrapper uncovered). Mutation check: renaming the version flag makes tests fail.
- go vet, gofmt -l, go test -race, and go build clean locally; ldflags stamping verified (v0.1.0).
- README, MIT LICENSE, .github/workflows/ci.yml (ubuntu-latest, setup-go from go.mod, gofmt check, vet, race tests, build). Workflow YAML parses; AC #5 stays unchecked until the first run on GitHub passes.
- DoD #8 (slog file logging) not applicable: logging setup belongs to TASK-003.
- No new dependencies (standard library only).
<!-- SECTION:NOTES:END -->
