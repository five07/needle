---
id: TASK-010
title: 'Search: API and view'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:36Z'
updated_date: '2026-11-22T14:45:05Z'
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
- [x] #1 Search returns tracks, albums, artists, and playlists, grouped by type
- [x] #2 Typing debounces requests and stale responses never overwrite newer results
- [x] #3 Search runs without interrupting or stuttering current playback
- [x] #4 From a result: Enter plays, a key adds as Play next, another adds to queue
- [x] #5 Opening an album, artist, or playlist shows its tracks with the same actions
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
Decisions 27-30 (all recommended options).
T1 internal/tidal/catalog.go: Catalog(country) with Search (GET /searchResults?filter[query], nested includes), AlbumTracks, PlaylistTracks, ArtistTracks (collapseBy=FINGERPRINT), ArtistAlbums; Page[T] with next links validated to the same path.
T2 shell: ForViewMsg/To routing to the owning view, Opener on push, Notices channel, notice text sanitized, hint line drops whole hints that do not fit.
T3 SearchView: 300 ms debounce, enter searches at once, per-request cancel, stale responses dropped.
T4 entryList: grouped rows with headings, tab/shift+tab, enter/a/A; collections queued up to 500 tracks; artists hint to open.
T5 TrackListView (album/playlist, lazy paging) and ArtistView (top tracks + albums, album paging).
T6 cmd/tidal-tui: queue + stream.Resolver + player.Player started with the UI and stopped after it; playback adapter; player messages forwarded as notices; README.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Live probe (user-run, 2026-11-22): search is GET /searchResults?filter[query]=..&countryCode=..&include=tracks.artists,tracks.albums,albums.artists,artists,playlists (the /searchResults/{query} path form returns 400 INVALID_RESOURCE_ID). Nested includes populate track artists/album and album artists. 20 items per group. Album items carry volume/track meta; playlist items carry itemId/addedAt and may include videos (skipped).
T1 done: internal/tidal/catalog.go (Catalog with Search, AlbumTracks, PlaylistTracks, ArtistTracks, ArtistAlbums; next links validated to the same path), tidal 99.4%.
T2-T5 done: ForViewMsg/To routing (results and debounce ticks reach their view after a section switch), Opener on push, notices channel, notice text sanitized; entryList (headings, tab groups, enter/a/A), SearchView (300 ms debounce, per-request cancel, stale drop), TrackListView (album/playlist, lazy paging), ArtistView (top tracks + albums). Hint line drops whole hints that do not fit instead of truncating. tui 97.7%.

Verification: go test -race ./... clean, go vet and gofmt clean. Coverage: internal/tidal 99.4%, internal/tui 97.7%, cmd/tidal-tui 96.5%. Mutation spot check 16/16 killed (debounce stale tick, stale response drop, request cancel, view routing, debounce routing, play start index, queue cap, pager error guard, pager loading guard, notice sanitize, heading skip, next-link path check, playlist video skip, play-next order, play-next on empty queue). E2E: TestRunSearchEndToEnd runs run() against a fake TIDAL server: search with countryCode from /users/me, results on screen, enter plays, player error returned as a notice. No new dependencies.
Not verified here: a live interactive run against TIDAL (needs the user's login; left for the user per their preference). AC #3 is shown by design and tests (API calls run in commands off the UI goroutine, the player has its own goroutine, search makes no playback calls), not by listening.

User reviewed the diff on 2026-11-22 and approved marking Done.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Search for TIDAL tracks, albums, artists, and playlists, with playback from results. internal/tidal gains a Catalog (search, album/playlist/artist track pages, artist albums) shaped by a live probe of the official API. internal/tui gains a debounced Search view with stale-response protection, grouped results with tab navigation, enter to play the surrounding list, a/A to queue (albums and playlists up to 500 tracks), and album, playlist, and artist detail views with lazy paging. The shell now routes results to the view that asked for them, runs an Open hook on pushed views, and shows external notices. cmd/tidal-tui starts the queue, resolver, and player with the UI (decision 27) and surfaces player errors as notices until the now-playing bar (TASK-013). Verified by unit, golden, and end-to-end httptest tests (-race), coverage 96.5% to 99.4%, and mutation checks. Pending: user review and a live run.
<!-- SECTION:FINAL_SUMMARY:END -->
