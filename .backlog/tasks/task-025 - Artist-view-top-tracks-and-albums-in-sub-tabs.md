---
id: TASK-025
title: 'Artist view: top tracks and albums in sub-tabs'
status: To Do
assignee: []
created_date: '2026-11-22T16:15:21Z'
labels:
  - tui
milestone: m-2
dependencies:
  - TASK-024
priority: low
type: enhancement
ordinal: 25000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The artist view still shows Top tracks and Albums as one grouped list with headings. Search, the Library, and Discovery use sub-tabs on the shared tab bar (decisions 40 and 46), and the user prefers that layout everywhere.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The artist view shows sub-tabs Top tracks and Albums on the shared tab bar; tab/shift+tab and clicks switch tabs
- [ ] #2 A tab with no items is left out
- [ ] #3 Each tab loads more pages as the cursor nears its end, as today
- [ ] #4 The hint line leads with tab, matching the other tabbed views
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
