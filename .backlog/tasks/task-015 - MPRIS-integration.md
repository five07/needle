---
id: TASK-015
title: MPRIS integration
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:36Z'
updated_date: '2026-11-22T18:02:28Z'
labels:
  - integration
  - linux
milestone: m-3
dependencies:
  - TASK-008
  - TASK-013
priority: low
type: feature
ordinal: 15000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Desktop media keys and bar widgets (Waybar, omarchy-shell) use MPRIS over D-Bus. In v1 per Q12. No seeking in v1 (Q8).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Player registers on the session bus as org.mpris.MediaPlayer2.tidal-tui
- [x] #2 PlayPause, Play, Pause, Next, Previous, and Stop from MPRIS control playback
- [x] #3 CanSeek is false and Seek/SetPosition are no-ops
- [x] #4 Metadata, PlaybackStatus, Shuffle, and LoopStatus update on track and state changes
- [x] #5 Absence of a session bus is logged and does not prevent startup
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
1. T1: player.SetPaused and playback.SetRepeat.
2. T2: internal/mpris service: bus name (with .instance<pid> fallback), MediaPlayer2 and Player interfaces, controls, no seeking (AC #1-3).
3. T3: property updates fed by watchStatus, PropertiesChanged for Metadata/PlaybackStatus/Shuffle/LoopStatus; startup without a session bus (AC #4, #5).
4. T4: README, DoD, mutation checks, manual playerctl check by the user.
Decisions 55-57: godbus direct; no album art; volume read-only.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
T1-T4 implemented. internal/mpris exports org.mpris.MediaPlayer2 and .Player on /org/mpris/MediaPlayer2 with godbus prop/introspect, takes org.mpris.MediaPlayer2.tidal-tui (or .instance<pid>), maps Play/Pause/PlayPause/Stop/Next/Previous and Shuffle/LoopStatus writes to the player; CanSeek false and Seek/SetPosition/OpenUri ignored; Volume read-only (decision 57); no album art (decision 56). The status watcher feeds it; only changed properties are announced; Position is stored without a signal. Player.SetPaused added so MPRIS Play and Pause are not toggles. No session bus: warning logged, app runs. Tests use a private dbus-daemon. Coverage: mpris 96.6%, player 95.6%, cmd 96.3%. 19 mutations, all killed after two test fixes. Manual playerctl check handed to the user. Awaiting user review.

User approved closing.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
tidal-tui registers on the D-Bus session bus as org.mpris.MediaPlayer2.tidal-tui (or .instance<pid> for a second copy) with the MPRIS root and Player interfaces. Play, Pause, PlayPause, Stop, Next, Previous, and Shuffle/LoopStatus writes control playback; CanSeek is false and Seek/SetPosition/OpenUri are ignored; Volume is read-only (decision 57); no album art (decision 56). Metadata, PlaybackStatus, Shuffle, and LoopStatus are announced only when they change; Position is answered on request. Without a session bus a warning is logged and the app runs. Player.SetPaused was added so Play and Pause are not toggles. godbus/dbus/v5 became a direct dependency (decision 55). Verified: go test -race, vet and gofmt clean; mpris 96.6%, player 95.6%, cmd 96.3%; all 19 mutations caught. The manual busctl check was handed to the user.
<!-- SECTION:FINAL_SUMMARY:END -->
