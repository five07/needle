---
id: TASK-023
title: Fade out audio before quitting
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T15:25:28Z'
updated_date: '2026-11-22T15:45:46Z'
labels:
  - player
  - audio
milestone: m-2
dependencies:
  - TASK-008
priority: high
type: bug
ordinal: 23000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Quitting while a track plays makes an audible pop: mpv is told to quit while audio is still flowing, so the output stream is cut mid-waveform. On a loud system, or in exclusive ALSA output, this is jarring. Agreed fix: ramp the volume down instead of a plain mute (a one-step mute can click too). Changing volume at quit time does not affect bit-perfect playback, which only matters while playing.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 When a track is playing at quit, the output volume ramps to silence over about 100 ms before mpv is paused and quit, in both shared and exclusive output modes
- [x] #2 After the ramp, quitting waits long enough for audio already buffered in the output device to play out (about mpv's audio buffer, 200 ms by default) before pausing
- [x] #3 When nothing is playing (idle or paused), quitting does not ramp or wait
- [x] #4 If mpv does not answer during the fade, quitting still finishes within the existing quit timeout (mpv is killed as before)
- [x] #5 Quit by q, ctrl+c, and SIGINT all go through the fade
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
1. T1: fade in MPV.Close: query core-idle; if audible, read volume, ramp to 0 over FadeOut (~100 ms) in ~10 ms steps, wait FadeDrain (~200 ms), pause, then quit. All within QuitTimeout; any IPC error falls through to quit/kill. New MPVConfig fields FadeOut, FadeDrain with defaults.
2. T2: confirm ctx cancel in Player.Run reaches engine.Close (shared path for q, ctrl+c, SIGINT); add a test if missing.
3. T3: README line, DoD, listening check by the user.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fade implemented in MPV.Close (core-idle check, 20-step cube-root ramp from the current volume over FadeOut, mute if the volume is unreadable, FadeDrain wait, pause, quit). The fade has its own budget inside QuitTimeout, and the exit wait now uses the same deadline. Tests in internal/player/fade_test.go use the fake mpv command log. Fixed a test-only issue: -race children slept 1 s at exit (GORACE atexit_sleep_ms), so the fake mpv was always killed. AC #1 waits on the user's listening check in shared and exclusive modes.

User confirmed by listening: no pop on quit in shared and exclusive modes, with q and ctrl+c.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Quitting while a track played popped because mpv was told to quit mid-stream. MPV.Close now checks core-idle and, if audio is playing, ramps the volume to zero over about 100 ms (20 cube-root steps from the current level, or a one-step mute if the level is unknown), waits about 200 ms for the device buffer to drain, pauses, then quits. Idle or paused mpv quits at once. The fade has its own budget inside QuitTimeout, so a hung mpv is still killed within 2 s. Verified with fake-mpv command-order and stall tests (race-clean, player coverage 95.8%, 8/8 mutations caught) and the user's listening check in both output modes. Also fixed a test-only issue: -race fake mpv children slept 1 s at exit. Follow-up: SIGTERM/SIGHUP skip the fade because only SIGINT is caught.
<!-- SECTION:FINAL_SUMMARY:END -->
