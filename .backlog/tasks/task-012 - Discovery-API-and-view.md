---
id: TASK-012
title: 'Discovery: API and view'
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
priority: medium
type: feature
ordinal: 12000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md requires Discovery. THIRD_PARTY access covers personal mixes (userDailyMixes, userDiscoveryMixes, userNewReleaseMixes), track/artist radio, and similar tracks/artists/albums. Home feed, genres, and editorial are INTERNAL only (doc-001). Scope from Q9.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Discovery shows Daily, Discovery, and New Release mixes for the user
- [ ] #2 Each mix can be opened and its tracks played or queued
- [ ] #3 A Start radio action on any track or artist, from every view, opens its radio as a playable list
- [ ] #4 A Similar action on any track, artist, or album, from every view, lists similar items
- [ ] #5 Mixes the API returns empty are hidden, not shown as errors
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
