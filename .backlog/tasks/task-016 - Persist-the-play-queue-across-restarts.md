---
id: TASK-016
title: Persist the play queue across restarts
status: To Do
assignee: []
created_date: '2026-11-22T04:28:52Z'
updated_date: '2026-11-22T04:29:00Z'
labels:
  - player
dependencies:
  - TASK-006
priority: low
type: enhancement
ordinal: 16000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Deferred from v1 by PLAN.md Q11. Restarting the app currently loses the queue and position in it, so users have to rebuild it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Queue contents, current index, shuffle, and repeat state are restored on next launch
- [ ] #2 Tracks that are no longer available are skipped with a notice
- [ ] #3 Queue state is stored under $XDG_STATE_HOME/tidal-tui/
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
