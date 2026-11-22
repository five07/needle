---
id: TASK-022
title: Rename project to needle
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T15:06:38Z'
updated_date: '2026-11-22T20:12:34Z'
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
- [x] #1 Go module path is `github.com/five07/needle` and every import uses it
- [x] #2 The command lives in `cmd/needle/`, builds a `needle` binary, and `.gitignore` ignores `/needle`
- [x] #3 Config, state, log, and runtime paths use `needle` (`$XDG_CONFIG_HOME/needle/`, `$XDG_STATE_HOME/needle/needle.log`, `$XDG_RUNTIME_DIR/needle/`, temp prefix `needle-`)
- [x] #4 The keyring service is `needle`; the account key `tidal-account`, `TIDAL_CLIENT_ID`, and the `tidal` package keep their names
- [x] #5 The login callback page text and the TUI frame title say `needle`, and the golden files are regenerated and reviewed
- [x] #6 No code reads the old `tidal-tui` paths or keyring entry (no migration)
- [x] #7 README says once under the intro: "needle is an independent project and is not affiliated with or endorsed by TIDAL.", and `-version` prints the same line
- [x] #8 README, UI.md, the PLAN.md Definition of Done, and the backlog project name and Definition of Done use `needle`
- [x] #9 A grep for `tidal-tui` outside history (completed tasks, PLAN.md questions and decisions) finds nothing
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
- [x] #8 slog logging to $XDG_STATE_HOME/tidal-tui/ with track_id/request_id fields; no tokens logged
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. T1: module path github.com/five07/needle; cmd/tidal-tui moved to cmd/needle (plain mv); .gitignore and CI.
2. T2: runtime names (XDG dirs, needle.log, runtime dir and temp prefix, keyring service, callback page, frame title, error prefixes, MPRIS bus name/identity/track paths, User-Agent); goldens regenerated and reviewed; no reads of old paths.
3. T3: affiliation notice in README and -version.
4. T4: README, UI.md, PLAN.md DoD, backlog project name and DoD.
5. T5: DoD checks, report.
Assumptions: Q&A history and done tasks keep tidal-tui; cmd/tidal-spike keeps its name.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
T1-T5 implemented. Module github.com/five07/needle; cmd/tidal-tui moved to cmd/needle with mv (git add -A sees the rename); .gitignore /needle; CI builds ./cmd/needle. Runtime names: XDG dirs, needle.log, $XDG_RUNTIME_DIR/needle and needle- temp prefix, keyring service needle (account tidal-account kept), callback page, frame title, window title, error prefixes, mpv audio client name, User-Agent, MPRIS bus name org.mpris.MediaPlayer2.needle, identity, and track paths. 30 goldens regenerated: only the top border title changed. No code reads the old paths or keyring entry. Affiliation line in README and -version (tested). Docs: README, UI.md, PLAN.md intro and DoD; backlog projectName via CLI; the two DoD strings in .backlog/config.yml edited directly (no CLI for the DoD). PLAN.md Q&A history and finished task text keep tidal-tui; cmd/tidal-spike keeps its name. Awaiting user review.

User reviewed and approved; the local folder is now ~/Projects/Go/needle and the rename is committed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
The project is renamed to needle. The Go module is github.com/five07/needle; the command lives in cmd/needle and builds a needle binary (.gitignore and CI updated). Runtime names use needle: XDG config and state directories, needle.log, $XDG_RUNTIME_DIR/needle and the needle- temp prefix, the keyring service (account key tidal-account kept), the login callback page, the TUI frame and window title, error messages, mpv's audio client name, the User-Agent, and the MPRIS bus name, identity, and track paths. No code reads the old paths or keyring entry (decision 37). The affiliation notice is in the README and printed by -version (decision 39). README, UI.md, the PLAN.md intro and DoD, and the backlog project name and DoD use needle; PLAN.md history and finished tasks keep tidal-tui. Verified: go test -race, vet and gofmt clean; coverage unchanged; 30 goldens changed only in the title. Left to the maintainer: the GitHub repo and remote, the TIDAL app name, and Waybar or omarchy-shell config.
<!-- SECTION:FINAL_SUMMARY:END -->
