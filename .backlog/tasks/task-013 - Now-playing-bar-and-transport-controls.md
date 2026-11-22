---
id: TASK-013
title: Now-playing bar and transport controls
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:36Z'
updated_date: '2026-11-22T17:14:29Z'
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
- [x] #1 Bar shows title, artist, album, elapsed/total time, and a progress indicator (display only, no seeking). The song title is 2x size using the kitty text-sizing protocol (OSC 66), falling back to bold where unsupported. If the bar is not wide enough, the title scrolls across the view endlessly.
- [x] #2 Bar shows the received format with bit depth and sample rate read from the stream (for example FLAC 24/192), and flags when output format differs from source
- [x] #3 Bar shows shuffle and repeat state
- [x] #4 Keys: space toggles play/pause, n/] next track, p/[ previous track, s shuffle, r cycles repeat
- [x] #5 Transport controls in the bar are clickable with the mouse
- [x] #6 A queue view lists upcoming tracks and supports remove and clear
- [x] #7 Every line of the bar is centered, top to bottom: title (2 rows), artist and album, format info on its own line, progress, then controls (shuffle, previous/play-pause/next, repeat), as drawn in UI.md
- [x] #8 Pressing enter on the track that is currently playing toggles play/pause instead of restarting it; enter on any other track keeps the existing behavior
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
1. T1: player status reaches the UI (Transport interface, status messages, wiring).
2. T2: bar rendering: title (bold on 2 rows, marquee), artist and album, format and indicator, progress, controls (AC #1-3, #7).
3. T3: transport keys space n ] p [ s r, clickable controls (AC #4, #5).
4. T4: Queue view: current then upcoming; enter jumps, d/x remove, D clears with a second-press confirm; sidebar count (AC #6).
5. T5: enter on the current track toggles pause (AC #8).
6. T6: README, DoD, mutation checks, report.
Decisions 47-49. 2x title split to its own task.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
T1-T6 implemented. Player status and the queue reach the UI as NowPlaying snapshots through one watcher (the player's Updates channel is coalesced, so it has a single reader); queue edits signal it too; the newest snapshot replaces one the UI has not taken. tui.Transport is implemented by the playback adapter. Bar: bold title on 2 rows with a cell-based marquee (decision 47; 2x title split to TASK-026), artists and album, format line with source, output, mode, and indicator (yellow with the reason when altered), progress, clickable controls. Keys space n ] p [ s r outside text inputs; Playback group in help. Queue view: current then upcoming, enter jump or pause, d/x remove, D clear with a second-press confirm (decision 49), sidebar count. Enter on the current track toggles pause when playing or paused, by track ID in any list (decision 48). Coverage: tui 99.4%, cmd 97.1%. 20 mutations, all killed after two test fixes. Awaiting user review.

User reviewed and approved.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
The now-playing bar, transport controls, and Queue view are in place. The bar shows, centered: the bold title on 2 rows (it scrolls when too wide; the 2x title is TASK-026), artists and album, a format line with source, output, mode, and the bit-perfect indicator (yellow with the reason when the output differs), a display-only progress bar, and clickable shuffle/previous/play-pause/next/repeat controls. Keys space, n/], p/[, s, r work outside text inputs. The Queue view lists the current then upcoming tracks; enter jumps or toggles pause, d/x remove, D clears after a second press within 2 s; the sidebar shows the upcoming count. Enter on the playing or paused track toggles pause, in any list. Player status and the queue reach the UI as snapshots through one watcher. Verified: go test -race, vet and gofmt clean; tui coverage 99.4%, cmd 97.1%; all 20 mutations caught.
<!-- SECTION:FINAL_SUMMARY:END -->
