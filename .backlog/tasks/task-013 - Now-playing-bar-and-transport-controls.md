---
id: TASK-013
title: Now-playing bar and transport controls
status: To Do
assignee: []
created_date: '2026-11-22T03:53:36Z'
updated_date: '2026-11-22T05:37:59Z'
labels:
  - tui
  - player
milestone: m-2
dependencies:
  - TASK-008
  - TASK-006
  - TASK-009
references:
  - UI.md
priority: high
type: feature
ordinal: 13000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md requires showing title, artist, and bit depth/quality, plus play, pause, rewind, and advance controls.

Layout: check `UI.md` before implementing. Section 1 has the agreed screen mockup and section 3 has the now-playing bar details.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Bar shows title, artist, album, elapsed/total time, and a progress indicator (display only, no seeking). The song title is 2x size using the kitty text-sizing protocol (OSC 66), falling back to bold where unsupported. If the bar is not wide enough, the title scrolls across the view endlessly.
- [ ] #2 Bar shows the received format with bit depth and sample rate read from the stream (for example FLAC 24/192), and flags when output format differs from source
- [ ] #3 Bar shows shuffle and repeat state
- [ ] #4 Keys: space toggles play/pause, n/] next track, p/[ previous track, s shuffle, r cycles repeat
- [ ] #5 Transport controls in the bar are clickable with the mouse
- [ ] #6 A queue view lists upcoming tracks and supports remove and clear
- [ ] #7 Every line of the bar is centered, top to bottom: title (2 rows), artist and album, format info on its own line, progress, then controls (shuffle, previous/play-pause/next, repeat), as drawn in UI.md
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
