---
id: TASK-003
title: Config and XDG paths
status: To Do
assignee: []
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T04:28:52Z'
labels:
  - infra
milestone: m-0
dependencies:
  - TASK-002
priority: high
type: feature
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Settings need a stable home that follows XDG conventions, and logs cannot go to stdout in a TUI. Settings decided in PLAN.md: quality aac/lossless/hires (Q6), output device and shared/exclusive mode (Q5), OAuth client ID override (Q2).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Config is read from $XDG_CONFIG_HOME/tidal-tui/config.toml with documented defaults when absent
- [ ] #2 Supported keys: quality (aac, lossless, hires; default hires), audio.mode (shared, exclusive; default shared), audio.device (default system default), auth.client_id (default embedded ID)
- [ ] #3 Invalid config values produce a clear error naming the key, without crashing
- [ ] #4 slog logs are written to a file under $XDG_STATE_HOME/tidal-tui/
- [ ] #5 Tests cover defaults, overrides, invalid values, and unset XDG variables
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
