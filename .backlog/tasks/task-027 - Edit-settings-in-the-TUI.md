---
id: TASK-027
title: Edit settings in the TUI
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T17:46:12Z'
updated_date: '2026-11-22T20:10:20Z'
labels:
  - tui
  - post-mvp
dependencies:
  - TASK-014
priority: low
ordinal: 27000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Account view (TASK-014) shows the quality, audio mode, and audio device settings read-only, and they can only be changed in config.toml followed by a restart. Let the user change them from the Account view.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The quality, audio mode, and audio device can be changed from the Settings section
- [x] #2 The device choice lists the devices needle -list-devices shows for the selected mode
- [x] #3 Changes are saved to config.toml, keeping the file's other settings and comments
- [x] #4 Changes apply without restarting, or the view says clearly that a restart is needed
- [x] #5 The autoplay setting (decision 62) can be changed from the Settings section
- [x] #6 Settings chosen in the Settings section (TASK-020) are saved to config.toml
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
TASK-017 added the autoplay config setting; the user wants it editable in the Account view as part of this task.

TASK-020 creates the Settings section (decision 70) with the read-only quality, audio mode, device, and autoplay rows moved from Account, and session-only visualizer on/off and theme. This task makes them editable and saved.

Implemented (decisions 74-81). config.Edit: line editor that rewrites only changed keys (trailing comments kept) or adds them under their table, refuses dotted keys/inline tables/multi-line values (UnsupportedError), validates with Load and checks the new values before an atomic replace keeping the mode (0600 new), writes through symlinks. stream.Resolver.SetQuality (drops the cache; older fetches not cached). player: DropPreload (epoch ignores in-flight preloads), SetAudio (closes the engine, swaps mode/engine factory/prober, reloads the current track at its position, paused stays paused), Status.Mode (the bar follows a switch). cmd settingsService: quality/autoplay/visualizer apply at once; mode/device switch and saves after 1 s without changes; Close saves at quit without switching; save failures notify and log; Devices as -list-devices (auto first in shared). TUI: Settings rows editable, quality notice, device picker (exclusive only with a device picked, esc cancels), "(now on/off)" for v/c session changes. Tests: config edit goldens byte for byte, resolver/player switch tests, settings service with a fake timer, TUI picker flows, end-to-end save from key presses. Mutation checks: all survivors killed.

Verified 2026-11-22: go test -race ./... passes (cmd/needle 96.3%, config 96.7%, player 95.7%, stream 100%, tui 99.4%), go vet and gofmt -l clean, go build ./cmd/needle succeeds and the binary starts. Finalized at the user's request.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Settings section rows are now editable and saved to config.toml. Quality, audio mode, device, autoplay, visualizer on/off and theme change from Settings (decision 74). A small line editor in internal/config rewrites only the changed keys, keeping comments and layout, validates with Load, and replaces the file atomically (decision 75). Quality applies from the next track with a notice (78). Mode and device switch live: the engine restarts and resumes the current track at its position, debounced 1 s (79, 80). The device picker lists the devices -list-devices shows for the mode. v and c stay session-only toggles, shown as (now on/off) (77). Risk: a live mode or device switch fails visibly if the device is busy. Keys written as dotted keys or inline tables are not rewritten; the change applies for the session and the view says it could not be saved.
<!-- SECTION:FINAL_SUMMARY:END -->
