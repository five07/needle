---
id: TASK-025
title: 'Artist view: top tracks and albums in sub-tabs'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T16:15:21Z'
updated_date: '2026-11-22T16:31:20Z'
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
- [x] #1 The artist view shows sub-tabs Top tracks and Albums on the shared tab bar; tab/shift+tab and clicks switch tabs
- [x] #2 A tab with no items is left out
- [x] #3 Each tab loads more pages as the cursor nears its end, as today
- [x] #4 The hint line leads with tab, matching the other tabbed views
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
1. T1: ArtistView with Top tracks and Albums tabs on the shared tab bar; empty tabs left out; albums page lazily; top tracks one page; first arriving list selected, later arrival keeps the current tab; HintLeader with tab.
2. T2: remove the now-unused group heading code (entryHeading, jumpGroup, List.heading, bindings groups flag).
3. T3: README, DoD, mutation checks, report.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
T1-T3 implemented. ArtistView keeps one entryList per part (Top tracks, Albums) on the shared tab bar; empty parts get no tab; the first part to arrive is shown and a later arrival leaves the shown tab and cursor alone. Albums page lazily; top tracks stay one page (AC #3 'as today'). HintLeader leads with tab when both tabs exist. T2 removed the unused group code: entryHeading/headingEntry, entry.group/heading, skip, jumpGroup, List.heading, the bindings groups flag; NextGroup/PrevGroup renamed NextTab/PrevTab. tui coverage 99.2%, detail.go 100%. 12 mutations: 8 killed; 2 survivors were dead checks, now removed; 2 equivalent (tab direction with two tabs, track filter in activate on single-kind lists). Awaiting user review.

User reviewed and approved.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
The artist view now shows Top tracks and Albums in sub-tabs on the shared tab bar. tab/shift+tab and clicks switch tabs, empty tabs are left out, and each tab keeps its own cursor. The first list to arrive is shown, and a later one never moves the user off their tab. Albums page lazily; top tracks stay one page. The hint line leads with tab when both tabs exist. The now-unused group heading code was removed from the shared list, and NextGroup/PrevGroup were renamed NextTab/PrevTab. Verified: go test -race, vet and gofmt clean; tui coverage 99.2%, detail.go 100%; 8 of 12 mutations caught, 2 survivors were dead checks (removed) and 2 change nothing a user can see.
<!-- SECTION:FINAL_SUMMARY:END -->
