---
id: TASK-022
title: Rename project to needle
status: To Do
assignee: []
created_date: '2026-11-22T15:06:38Z'
labels:
  - chore
  - post-mvp
dependencies:
  - TASK-011
  - TASK-012
  - TASK-013
  - TASK-014
  - TASK-015
priority: low
type: chore
ordinal: 22000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The product name `tidal-tui` puts the TIDAL trademark in the project name, which TIDAL may object to. The project becomes `needle`, still described as a terminal client for TIDAL (naming the service it works with is fine). Name availability was checked on 2026-11-22: no `needle` package in the AUR or Arch repos, and `github.com/five07/needle` was free. Decisions 37-39 in PLAN.md: no migration of existing config or credentials (the project has no releases), done after v1 is feature-complete, and an affiliation notice. The full scope list is in the "Rename to needle" section of PLAN.md. Renaming the GitHub repo, updating the git remote, and renaming the app at developer.tidal.com are left to the maintainer.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Go module path is `github.com/five07/needle` and every import uses it
- [ ] #2 The command lives in `cmd/needle/`, builds a `needle` binary, and `.gitignore` ignores `/needle`
- [ ] #3 Config, state, log, and runtime paths use `needle` (`$XDG_CONFIG_HOME/needle/`, `$XDG_STATE_HOME/needle/needle.log`, `$XDG_RUNTIME_DIR/needle/`, temp prefix `needle-`)
- [ ] #4 The keyring service is `needle`; the account key `tidal-account`, `TIDAL_CLIENT_ID`, and the `tidal` package keep their names
- [ ] #5 The login callback page text and the TUI frame title say `needle`, and the golden files are regenerated and reviewed
- [ ] #6 No code reads the old `tidal-tui` paths or keyring entry (no migration)
- [ ] #7 README says once under the intro: "needle is an independent project and is not affiliated with or endorsed by TIDAL.", and `-version` prints the same line
- [ ] #8 README, UI.md, the PLAN.md Definition of Done, and the backlog project name and Definition of Done use `needle`
- [ ] #9 A grep for `tidal-tui` outside history (completed tasks, PLAN.md questions and decisions) finds nothing
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
