---
id: TASK-011
title: 'Library: API and views'
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
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md requires browsing the user own playlists, albums, and so on. Write actions decided in Q10: favorite/unfavorite and add track to playlist. Library text search is not available to THIRD_PARTY (doc-001).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Library lists user playlists, favorite albums, favorite tracks, and favorite artists
- [ ] #2 Large collections paginate lazily while scrolling, sorted by date added by default
- [ ] #3 A filter key narrows the loaded items client-side
- [ ] #4 Items support play, Play next, and Add to queue
- [ ] #5 Favorite/unfavorite works on tracks, albums, artists, and playlists, from the library and from search results, and the view refreshes
- [ ] #6 Add to playlist lets the user pick one of their playlists and appends the track, reporting duplicates
- [ ] #7 Empty library states show a helpful message
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
