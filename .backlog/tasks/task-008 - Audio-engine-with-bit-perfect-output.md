---
id: TASK-008
title: Audio engine with bit-perfect output
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T06:48:13Z'
labels:
  - player
  - audio
milestone: m-1
dependencies:
  - TASK-007
documentation:
  - doc-003
priority: high
type: feature
ordinal: 8000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Core of the product: bit-perfect playback. Decided in PLAN.md: mpv subprocess over JSON IPC (Q3), Linux only (Q4), configurable output with shared PipeWire default and exclusive ALSA hw: mode (Q5), no seeking (Q8), best-effort gapless (Q13). doc-002 lists bit-perfect pitfalls seen in another client.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The engine launches mpv with a private JSON IPC socket and stops it cleanly on exit
- [x] #2 Play, pause, resume, and stop work on a manifest from task-007
- [x] #3 Shared mode outputs through PipeWire; exclusive mode opens the configured ALSA hw: device with mpv exclusive output
- [x] #4 In exclusive mode, output sample rate and bit depth match the source (verified on a 24/96 or 24/192 track)
- [x] #5 Source format and actual output format are reported so the UI can show resampling
- [x] #6 Bit-perfect indicator turns off when the negotiated output rate differs from the source rate
- [x] #7 Bit-perfect indicator turns off when software volume is below 100% (or software volume is disabled in exclusive mode)
- [x] #8 The next queue item is preloaded so consecutive tracks play gaplessly where the formats match
- [x] #9 Track-end events advance the queue
- [x] #10 A missing or crashed mpv process yields a user-visible error, not a hang
- [x] #11 Engine is behind an interface with a fake used in tests
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
Package internal/player (standard library only unless Q17c is chosen).
T1 mpv process and IPC: launch mpv --idle=yes --no-config --no-video --no-terminal with a private socket in $XDG_RUNTIME_DIR/tidal-tui/ (0700; short MkdirTemp fallback, 108-byte path limit), --gapless-audio=weak (never yes), --prefetch-playlist=yes, --volume=100, --replaygain=no, the DASH protocol whitelist, and per mode: exclusive --ao=alsa --audio-device=<hw> --audio-exclusive=yes; shared --ao=pipewire --audio-device=<device|auto>. JSON IPC client with request ids, property observers, and events. MPDs written 0600 to the private dir and removed after use. mpv missing -> typed error; process exit or socket loss -> crash event, never a hang; Close stops mpv (quit, then kill after a timeout).
T2 Engine interface + mpv implementation + fake: Play(item), Preload(next) (playlist append), Pause/Resume, Stop, Events (TrackStarted, TrackEnded{eof|error|stopped}, OutputChanged, Failed), Status.
T3 Output probe and bit-perfect evaluation: read /proc/asound/cardN/pcmDp/subS/hw_params; exclusive card from the hw: device name, shared card from the PipeWire sink (pw-dump alsa.card/alsa.device; filter sinks -> unknown). Pure Evaluate(source, mpv params, hw params, volume, filters, mode) -> state + reason per Q16. Busy device -> typed error per Q17. Source bit depth per Q18.
T4 Player coordinator: queue.Queue + stream.Resolver + Engine. Track end advances the queue (Advance/stop at end); on track start, prefetch the next manifest and Preload it; segment/URL expiry error mid-track -> Invalidate, re-resolve, resume at position; errors surfaced via stream.Message; Status for the UI (track, source format, output format, indicator).
T5 Config/CLI per Q19 and Q20; README Playback/audio section.
Tests: fake engine for the coordinator; fake mpv via a test helper process speaking the IPC protocol (launch, events, crash, missing binary); table tests for hw_params parsing, pw-dump mapping, and Evaluate; -race. Final live check with the DAC for AC3, AC4, AC6.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Audio spike 2026-11-22 with a Schiit Vestri recorded in doc-003. Questions 16-20 added to PLAN.md; plan pending approval.

User answered Q16-Q20 with the recommended options (PLAN.md DECISIONS 16-20). Q20 follow-up tracked as post-MVP TASK-018.

Implemented T1-T5 in internal/player (ipc.go, mpv.go, proc_linux.go, engine.go, output.go, player.go, status.go, devices.go), Manifest.BitDepth in internal/tidal, exclusive device validation (config.ParseALSAHW) in internal/config, and -list-devices in cmd/tidal-tui.

Verification: go test -race ./... clean; player 95.9%, config 97.6%, tidal 99.3%, cmd 98.1%. Fake mpv via test-binary re-exec covers launch, events, busy, stream error, crash, missing binary, unresponsive quit. Player tests stress-run 25x under -race. Mutation spot check: 15/15 killed after adding two tests.

Live check 2026-11-22 on the Schiit Vestri (temporary build-tagged test, deleted): exclusive 24/192 -> card S32_LE 192 kHz bit-perfect, 24/96 -> S32_LE 96 kHz bit-perfect, volume disabled; shared via pipewire sink -> rate matched at 192 and 96 kHz.

Decisions during implementation: shared mode passes --ao=pipewire explicitly (per plan T1), so pulse/ devices are not supported; preload starts about 45 s before track end rather than at track start; events for a playlist entry that arrive before its loadfile reply are buffered and replayed; pw-dump card names are validated before use as a /proc path component.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added the audio engine (internal/player): mpv as a private subprocess over JSON IPC, exclusive ALSA hw: and shared PipeWire output, gapless preloading of the next queue item, queue advance on track end, one resume after a mid-track stream failure, and clear errors for a missing or crashed mpv and a busy DAC. The bit-perfect indicator compares the source (codec, rate, bit depth from the MPD) with the card's real hw_params, so PipeWire resampling is caught; it fails closed when the output is unknown. Exclusive mode requires an explicit alsa/hw: device (validated at startup) and disables software volume. New -list-devices flag. Verified with unit tests (-race, 95.9% coverage, mutation checks) and live on a Schiit Vestri in both modes. Follow-ups: TASK-013 consumes Player.Status; TASK-018 tracks the volume choice.
<!-- SECTION:FINAL_SUMMARY:END -->
