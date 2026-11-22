---
id: TASK-017
title: Autoplay radio when the queue ends
status: To Do
assignee: []
created_date: '2026-11-22T04:28:52Z'
updated_date: '2026-11-22T04:29:00Z'
labels:
  - player
  - api
dependencies:
  - TASK-006
  - TASK-012
priority: low
type: enhancement
ordinal: 17000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Deferred from v1 by PLAN.md Q11. With repeat off, playback stops at the end of the queue; some users want it to continue with related music.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 An autoplay setting (default off) continues with track radio for the last played track when the queue ends
- [ ] #2 Autoplayed tracks are marked as such in the queue view
- [ ] #3 Turning autoplay off mid-radio stops after the current track
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
