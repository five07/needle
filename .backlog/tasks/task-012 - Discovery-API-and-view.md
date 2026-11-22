---
id: TASK-012
title: 'Discovery: API and view'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:36Z'
updated_date: '2026-11-22T16:12:08Z'
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
- [x] #1 Discovery shows Daily, Discovery, and New Release mixes for the user
- [x] #2 Each mix can be opened and its tracks played or queued
- [x] #3 A Start radio action on any track or artist, from every view, opens its radio as a playable list
- [x] #4 A Similar action on any track, artist, or album, from every view, lists similar items
- [x] #5 Mixes the API returns empty are hidden, not shown as errors
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
Decisions 40-43: sub-tabs (Daily, Discovery, New Releases), reload after 1 hour, f/F allowed on mixes, R radio / S similar.
1. T1: internal/tidal/discovery.go: Mixes(kind), TrackRadio/ArtistRadio, SimilarTracks/Artists/Albums; Playlist gains Description and Mix; locale from LANG. httptest tests from probe documents.
2. T2: extract the Library tab bar into a shared helper; DiscoveryView with sub-tabs, empty tabs hidden, reload after 1 hour (clock in Services), goldens.
3. T3: R radio from every view via entryList: fetch the mix, push its track list; notice on unsupported rows.
4. T4: S similar from every view: SimilarView with lazy paging; notice on unsupported rows.
5. T5: wiring in cmd/tidal-tui, e2e, README, DoD, mutation checks, report.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
T1-T5 implemented. tidal: discovery.go (Mixes per kind with locale and page cap, TrackRadio/ArtistRadio with ErrNoRadio, SimilarTracks/Artists/Albums), Playlist.Description and Mix. tui: shared tabBar/tabAt (Library refactored onto it, goldens unchanged), DiscoveryView with sub-tabs and an hour-based reload via a new Revisiter hook, TrackListView generalized to page entries, NewRadioView and NewSimilarView, R/S in entryList. cmd: Discovery wired, locale from LC_ALL/LC_MESSAGES/LANG. Coverage: tidal 99.5%, tui 98.8%, cmd 96.2%. 18 mutations, all killed (one after adding TestTabAt). Awaiting user review.

User reviewed and approved.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added the Discovery section and the radio and similar actions. The tidal package gained Mixes (daily, discovery, new release; locale from the system language; page cap), TrackRadio and ArtistRadio (ErrNoRadio when none), and SimilarTracks/Artists/Albums; Playlist gained Description and Mix. The TUI shows mixes in sub-tabs (empty kinds hidden) on a tab bar now shared with the Library, reloads them on a visit more than an hour after the last load, and opens mixes like playlists, with favorites allowed. R opens a track or artist radio and S lists similar tracks, artists, or albums from every view, with notices on unsupported rows. Verified: go test -race, vet and gofmt clean; coverage tidal 99.5%, tui 98.8%, cmd 96.2%; e2e test against a fake TIDAL server covers loading mixes with the locale; 18/18 mutations caught. Follow-up: TASK-024 moves Search to sub-tabs.
<!-- SECTION:FINAL_SUMMARY:END -->
