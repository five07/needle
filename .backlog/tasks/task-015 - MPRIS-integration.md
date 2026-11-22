---
id: TASK-015
title: MPRIS integration
status: To Do
assignee: []
created_date: '2026-11-22T03:53:36Z'
updated_date: '2026-11-22T04:28:53Z'
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
- [ ] #1 Player registers on the session bus as org.mpris.MediaPlayer2.tidal-tui
- [ ] #2 PlayPause, Play, Pause, Next, Previous, and Stop from MPRIS control playback
- [ ] #3 CanSeek is false and Seek/SetPosition are no-ops
- [ ] #4 Metadata, PlaybackStatus, Shuffle, and LoopStatus update on track and state changes
- [ ] #5 Absence of a session bus is logged and does not prevent startup
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
