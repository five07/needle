---
id: TASK-010
title: 'Search: API and view'
status: To Do
assignee: []
created_date: '2026-11-22T03:53:36Z'
updated_date: '2026-11-22T04:28:53Z'
labels:
  - api
  - tui
milestone: m-2
dependencies:
  - TASK-005
  - TASK-009
  - TASK-006
documentation:
  - doc-001
priority: high
type: feature
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md requires search, usable while music plays.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Search returns tracks, albums, artists, and playlists, grouped by type
- [ ] #2 Typing debounces requests and stale responses never overwrite newer results
- [ ] #3 Search runs without interrupting or stuttering current playback
- [ ] #4 From a result: Enter plays, a key adds as Play next, another adds to queue
- [ ] #5 Opening an album, artist, or playlist shows its tracks with the same actions
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
