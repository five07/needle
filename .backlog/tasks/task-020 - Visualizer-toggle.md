---
id: TASK-020
title: Visualizer toggle
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T06:54:53Z'
updated_date: '2026-11-22T19:42:25Z'
labels:
  - tui
  - post-mvp
dependencies:
  - TASK-013
priority: low
ordinal: 20000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md New Items 3: an optional audio visualizer the user can turn on and off. It must not alter the audio path, so the bit-perfect indicator is unaffected. It should be a background for the now playing view. The theme should be selectable in a new "Settings" side bar controller. Ask me about this, all of our current and future non-TIDAL settings should move here. THis will get updated in TASK-027 and created there.
The themes are "terminal" (based on the current terminal color settings), "burnt orange", "ice blue", "forest green", "royal purple", "deep red" and "custom" (only editable in the config file)
Regardless of theme, a gradient will be used, darker at the bottom and bighter at the top, perhaps in 4 steps.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A key toggles the visualizer on and off
- [x] #2 Turning the visualizer on does not change the bit-perfect indicator state
- [x] #3 The layout stays usable at 80x24 with the visualizer on
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented (decisions 67-73). internal/visualizer: Analyzer (Hann + radix-2 FFT, 32 log bands 40 Hz-10 kHz, -60..0 dB, instant rise, 1.5/s fall); mpvDecoder (second silent mpv, --no-config, s16 mono 22.05 kHz PCM on stdout, 2 s buffer cap, manifest copy 0600 in the private runtime dir, removed on close, Pdeathsig); Controller (20 fps, follows Status position, restarts on track change or seek, skips when behind, waits when ahead, decays and sleeps when paused/stopped/off, a failed track is not retried). player: Status.Handle, read-only Manifest(handle) copy. cmd: visSource adapter, watcher wakes the controller, startVisualizer with PrivateDir. TUI: bars in the free cells of the now-playing bar (one blank cell kept beside text), 4-step gradient over 6 rows, themes, v key, Settings section (6) with visualizer rows editable for the session and audio/autoplay rows moved from Account. Sidebar scrolls at 50x16 so six sections fit; compact Settings glyph is ≡ (decision 73). Tests: AC1 TestVisualizerKey; AC2 TestManifestDoesNotTouchPlayback, TestSpectrumKeepsText; AC3 spectrum/settings goldens at 80x24 and 50x16. Mutation checks run; remaining survivors are equivalent (zero-frame fast path, anchor key check).

Reviewed and approved by the user. Theme colors may be tweaked later.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Optional spectrum visualizer as the now-playing bar background, from a second silent mpv decode, so the audio path and the bit-perfect indicator are untouched. v toggles it anywhere. New Settings section (6) with visualizer on/off and theme (terminal, five named themes, custom from config.toml), plus the audio and autoplay settings moved from Account. 4-step gradient over 6 rows. Sidebar scrolls at the 50x16 minimum (decision 73). Tradeoff: each track is downloaded twice while the visualizer is on (README).
<!-- SECTION:FINAL_SUMMARY:END -->
