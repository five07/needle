---
id: TASK-008
title: Audio engine with bit-perfect output
status: To Do
assignee: []
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T04:28:53Z'
labels:
  - player
  - audio
milestone: m-1
dependencies:
  - TASK-007
documentation:
  - doc-002
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
- [ ] #1 The engine launches mpv with a private JSON IPC socket and stops it cleanly on exit
- [ ] #2 Play, pause, resume, and stop work on a manifest from task-007
- [ ] #3 Shared mode outputs through PipeWire; exclusive mode opens the configured ALSA hw: device with mpv exclusive output
- [ ] #4 In exclusive mode, output sample rate and bit depth match the source (verified on a 24/96 or 24/192 track)
- [ ] #5 Source format and actual output format are reported so the UI can show resampling
- [ ] #6 Bit-perfect indicator turns off when the negotiated output rate differs from the source rate
- [ ] #7 Bit-perfect indicator turns off when software volume is below 100% (or software volume is disabled in exclusive mode)
- [ ] #8 The next queue item is preloaded so consecutive tracks play gaplessly where the formats match
- [ ] #9 Track-end events advance the queue
- [ ] #10 A missing or crashed mpv process yields a user-visible error, not a hang
- [ ] #11 Engine is behind an interface with a fake used in tests
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
