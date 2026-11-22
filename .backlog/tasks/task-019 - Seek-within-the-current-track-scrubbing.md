---
id: TASK-019
title: Seek within the current track (scrubbing)
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T06:54:53Z'
updated_date: '2026-11-22T20:48:43Z'
labels:
  - player
  - tui
  - post-mvp
dependencies:
  - TASK-013
priority: low
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md New Items 2: jump within the playing track from the keyboard, with the step growing (5, 10, 30 s) the faster the key is pressed. v1 has no seeking (decision 8), and the left/right arrows switch panes (TASK-009, decision 26), so this needs its own keys. Previews from the current access tier are only 30 s long.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Two keys (not the arrows) seek backward and forward in the current track
- [x] #2 Repeated presses within a short interval grow the step from 5 s to 10 s to 30 s
- [x] #3 Seeking never crosses into the previous or next track
- [x] #4 The now-playing progress updates immediately after a seek
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 Every acceptance criterion has at least one test that fails without the change
- [x] #2 go test -race ./..., go vet ./..., and gofmt -l . are clean
- [x] #3 Coverage: changed non-UI packages 95%+, TUI packages 80%+ (teatest/golden); API code has httptest coverage against a fake TIDAL server
- [x] #4 Security self-review done (input validation, no token or internal leaks in errors, shared-state concurrency, no fail-open paths)
- [x] #5 README updated for behavior, config, keybinding, or tradeoff changes
- [x] #6 Only dependencies named in the ticket added; no out-of-scope changes
- [x] #7 go build ./cmd/needle succeeds and the binary starts
- [x] #8 slog logging to $XDG_STATE_HOME/needle/ with track_id/request_id fields; no tokens logged
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Approved with every recommended answer (PLAN.md Q89-95, decisions 89-95).
1. T1 player: Engine.Seek(key, position) (mpv `seek <s> absolute+exact`, current entry only); progress samples carry the engine seek count. Player.SeekBy/SeekTo clamp to [0, duration-3s], paused stays paused, publish the target at once (SeekID++), send at most one engine seek per 150 ms (latest target), ignore older samples, drop a waiting seek on track change, stop, engine failure, or audio switch. Previews bound by the 30 s manifest duration.
2. T2 TUI: `,` and `.` (global, not while typing); run-based step 5/5/10/10/30 s with a 400 ms window, direction change resets; notice "» 10 s · 1:42"; bar position updated from the answer, late answers ignored; help lists both keys.
3. T3 cmd: playback.SeekBy implements tui.Seeker; player.ErrNotSeekable maps to tui.ErrNotSeekable.
4. T4 MPRIS: CanSeek while playing or paused; Seek (clamped offset) and SetPosition (current track, inside its length) per decision 92; Seeked emitted when the player SeekID changes.
5. T5 docs: README (keys, Seeking section, MPRIS), UI.md (progress row, keymap, mouse), PLAN.md decisions 89-95, decision 8 superseded.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented T1-T5 as planned.
- Stale-sample guard is two-layered: the mpv engine drops a sample if a seek landed while it was being taken, and stamps each sample with its seek count; the Player ignores samples older than its own count or while a seek is waiting.
- MPRIS Seek past the end clamps instead of calling Next (spec deviation, per decision 92); documented in README.
- Verification: go test -race ./... ok; go vet and gofmt clean; seek tests stable over -count=10; coverage player 95.9%, mpris 96.7%, cmd/needle 96.5%, tui 99.4%; go build ./cmd/needle and needle -version ok.
- Not exercised against a real mpv/DAC in this session: the fake mpv covers the IPC; a manual check with real playback is worth doing.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Seeking within the current track. `,`/`.` seek from any view (not while typing) with a step that grows 5, 10, 30 s within a run of presses under 400 ms apart; the bar shows the new position at once and a notice shows the step and position. The player clamps to 0:00 and 3 s before the end so seeking never changes track, keeps a paused track paused, combines presses into one mpv seek per 150 ms, and ignores position samples from before a seek. MPRIS now supports Seek, SetPosition, CanSeek, and Seeked. Decision 8 is superseded by decisions 89-95.

Verification: go test -race ./..., go vet, gofmt clean; changed packages 95.9-99.4% coverage; binary builds and starts. Tested against the fake mpv only; a manual check with real playback (especially exact seeks on FLAC_HIRES DASH streams) is the remaining risk.
<!-- SECTION:FINAL_SUMMARY:END -->
