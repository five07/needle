---
id: TASK-024
title: 'Search: results in sub-tabs'
status: To Do
assignee: []
created_date: '2026-11-22T15:57:31Z'
labels:
  - tui
milestone: m-2
dependencies:
  - TASK-012
priority: medium
type: enhancement
ordinal: 24000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Search results are one grouped list today (Tracks, Albums, Artists, Playlists with headings, tab jumps between groups). The user prefers the sub-tab layout used by the Library and Discovery (decision 40), so Search should use it too, with the same shared tab bar.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Search results show sub-tabs Tracks, Albums, Artists, Playlists; tab/shift+tab and clicks switch tabs
- [ ] #2 Tabs with no results are left out, and the first tab with results is selected after each search
- [ ] #3 The hint line leads with the most important Search keys, including tab, so they stay visible on small terminals, matching the Library
- [ ] #4 Enter on a track still queues the list it is in (now the Tracks tab), starting at that track
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
