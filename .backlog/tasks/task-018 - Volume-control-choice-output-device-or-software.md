---
id: TASK-018
title: 'Volume control choice: output device or software'
status: To Do
assignee: []
created_date: '2026-11-22T06:16:23Z'
labels:
  - player
  - post-mvp
dependencies:
  - TASK-008
  - TASK-013
documentation:
  - doc-003
priority: low
type: enhancement
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Post-MVP. Decided for v1 (PLAN.md Q20): exclusive mode disables software volume and the UI says volume is set on the DAC or amp; shared mode allows software volume, and the indicator shows "rate matched" only at 100%.

The user asked for a later setting that chooses how volume works, for example "volume on your output device" versus "let this computer's volume also control playback". Choosing software volume in exclusive mode must turn the bit-perfect indicator off whenever volume is below 100%.

Worth evaluating at the same time: USB DACs that expose a hardware volume control (an ALSA mixer element) apply it inside the DAC, so the stream sent over USB stays bit-perfect. The Schiit Vestri has only a mute switch (doc-003), so this needs a DAC with a mixer to test.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A config setting selects the volume behavior: output device only (default, current v1 behavior) or software volume
- [ ] #2 With software volume chosen, volume keys work in exclusive mode and the bit-perfect indicator turns off below 100%
- [ ] #3 The UI states which volume behavior is active
- [ ] #4 Where the DAC exposes an ALSA hardware volume control, using it is evaluated and documented, including whether it keeps the USB stream bit-perfect
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
