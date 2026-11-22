---
id: TASK-024
title: 'Search: results in sub-tabs'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T15:57:31Z'
updated_date: '2026-11-22T16:22:38Z'
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
- [x] #1 Search results show sub-tabs Tracks, Albums, Artists, Playlists; tab/shift+tab and clicks switch tabs
- [x] #2 Tabs with no results are left out, and the first tab with results is selected after each search
- [x] #3 The hint line leads with the most important Search keys, including tab, so they stay visible on small terminals, matching the Library
- [x] #4 Enter on a track still queues the list it is in (now the Tracks tab), starting at that track
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
Decisions 44-46 (all a).
1. T1: SearchView keeps one entryList per kind (Tracks, Albums, Artists, Playlists); only non-empty kinds get a tab on the shared tab bar; each search selects the first tab with results; layout input / tab bar / separator; clicks on tab names switch; tests and goldens updated.
2. T2: HintLeader for Search (/ search, tab next tab); sidebar hint puts / search first on Search; tab/shift+tab in the input leave it and switch tabs.
3. T3: README, DoD, mutation checks, report. Artist view sub-tabs tracked separately.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
T1-T3 implemented. SearchView keeps one entryList per kind on the shared tab bar; tabs for empty kinds are left out; each search selects the first tab. HintLeader leads with / search and tab; the shell skips its own / search when the view lists it, and the sidebar puts / search first on Search. tab/shift+tab in the input leave it and switch tabs. Tests and 6 goldens updated (main and minimum only for the sidebar hint order). Coverage tui 99.2%. 10 mutations: 9 killed, 1 equivalent (dead focus check in LeadHints, removed). Artist view sub-tabs: TASK-025. Awaiting user review.

User reviewed and approved.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Search results now use sub-tabs (Tracks, Albums, Artists, Playlists) on the tab bar shared with the Library and Discovery. Tabs for empty kinds are left out, each new search opens the first tab with results, and each tab keeps its own cursor. tab/shift+tab and clicks switch tabs, and tab in the input leaves it and switches in one key (decision 45). The hint line leads with / search and tab next tab, and the sidebar hint puts / search first on Search (decision 44). Verified: go test -race, vet and gofmt clean; tui coverage 99.2%; 9 of 10 mutations caught, and the survivor was dead code, now removed. Follow-up: TASK-025 moves the artist view to sub-tabs.
<!-- SECTION:FINAL_SUMMARY:END -->
