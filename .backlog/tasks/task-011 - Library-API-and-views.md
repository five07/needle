---
id: TASK-011
title: 'Library: API and views'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:36Z'
updated_date: '2026-11-22T15:45:46Z'
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
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md requires browsing the user own playlists, albums, and so on. Write actions decided in Q10: favorite/unfavorite and add track to playlist. Library text search is not available to THIRD_PARTY (doc-001).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Library lists user playlists, favorite albums, favorite tracks, and favorite artists
- [x] #2 Large collections paginate lazily while scrolling, sorted by date added by default
- [x] #3 A filter key narrows the loaded items client-side
- [x] #4 Items support play, Play next, and Add to queue
- [x] #5 Favorite/unfavorite works on tracks, albums, artists, and playlists, from the library and from search results, and the view refreshes
- [x] #6 Add to playlist lets the user pick one of their playlists and appends the track, reporting duplicates
- [x] #7 Empty library states show a helpful message
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
Awaiting answers to PLAN.md Q31-Q36 and a user-run read-only probe of the library endpoints.

T1 tidal: library reads (internal/tidal/library.go): FavoriteTracks/Albums/Artists/Playlists pages from /userCollection*/me/relationships/items with sort and nested includes; OwnedPlaylists from /playlists?filter[owners.id]=me. Reuses Page[T] and the converters from catalog.go.
T2 tidal: library writes: AddFavorite/RemoveFavorite(kind, id) on the collection items (POST/DELETE, Idempotency-Key from the client); AddToPlaylist(playlistID, trackID) with onDuplicates FAIL, mapping the 409 to ErrAlreadyInPlaylist (Q33).
T3 tui: favorites state shared by every view (known-favorite set, heart marker, f/F per Q31), with notices and refresh of Library lists.
T4 tui: Library view: sub-tabs (Q34), lazy paging, empty states (AC7), sort key (Q36), play/queue via entryList.
T5 tui: client-side filter on / in Library (Q35), matching title/artist/album.
T6 tui: add-to-playlist picker modal (+ key): owned playlists, enter adds, esc cancels, duplicate notice.
T7 wiring and docs: Library view in cmd/tidal-tui, README keys and Library section, golden screens.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
T1-T7 implemented. tidal: library.go (favorites lists with allow-listed sorts, OwnedPlaylists, Add/RemoveFavorite, AddToPlaylist with onDuplicates=FAIL) and 409 sentinels. tui: LibraryView (sub-tabs, lazy paging with a generation counter, o sort, / filter with background load to 5000, accent folding), favorites state and f/F with a FavoriteMsg broadcast, + playlist picker, CloseMsg, Filterer, root views opened on first show. cmd: Library wired to the Catalog; e2e test covers favorite POST and Library load. Coverage: tidal 99.5%, tui 98.4%, cmd 96.5%. 21 mutations, all killed after two test additions. Awaiting user review.

Review follow-up: Library hint line leads with / filter and tab (HintLeader interface), so both stay visible on small terminals. User reviewed and approved.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added the Library section. The tidal package gained favorites lists (tracks, albums, artists, playlists) with sorts limited to a fixed list, owned playlists, add and remove favorites (an item that is already a favorite counts as success), and add to playlist with onDuplicates=FAIL. The TUI gained a Library view with Playlists, Albums, Tracks and Artists tabs that load on first visit and page lazily; o cycles the server sort; / filters the tab, ignoring case and accents, loading the rest in the background up to 5000 items. f/F favorite and unfavorite in every view, with a heart on known favorites and Library rows updating on change; + opens an owned-playlist picker that reports duplicates. The hint line leads with / filter and tab. Verified: go test -race, vet and gofmt clean; coverage tidal 99.5%, tui 98.4%, cmd 96.5%; e2e test against a fake TIDAL server covers a favorite POST and loading the Library; mutation spot checks all caught. Risk: favorite state is only known for items loaded or changed this session, because the API cannot check a single item.
<!-- SECTION:FINAL_SUMMARY:END -->
