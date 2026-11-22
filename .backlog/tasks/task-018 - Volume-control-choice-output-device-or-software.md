---
id: TASK-018
title: 'Volume control choice: output device or software'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T06:16:23Z'
updated_date: '2026-11-22T20:32:41Z'
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
- [x] #1 A config setting selects the volume behavior: output device only (default, current v1 behavior) or software volume
- [x] #2 With software volume chosen, volume keys work in exclusive mode and the bit-perfect indicator turns off below 100%
- [x] #3 The UI states which volume behavior is active
- [x] #4 Where the DAC exposes an ALSA hardware volume control, using it is evaluated and documented, including whether it keeps the USB stream bit-perfect
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
1. T1 config: [audio] volume = "device"|"software" (default device), validated, editable via config.Edit.
2. T2 player: software volume in exclusive mode when volume = software; AdjustVolume/SetVolume on the Run goroutine; SetVolumeControl applies live; back to device resets to 100%; level re-applied to a new engine. Policy moved out of the mpv engine.
3. T3 TUI: - and = keys with notices (tui.Volume service, ErrVolumeOnDevice); Exclusive volume row in Settings; vol N% in the bar below 100%; help and goldens.
4. T4 cmd + MPRIS: wiring; MPRIS Volume writes clamped to 0-1, NotSupported when on the device.
5. T5 docs: README, UI.md keymap, PLAN.md decisions 82-88, doc-004 (ALSA hardware volume), DoD #7/#8 renamed from tidal-tui.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented (decisions 82-88). Tests: AC1 TestLoad (invalid volume control), TestEditAddsMissingKeys/volume control, TestSettingsVolumeControl, TestSettingsSavedFromTheUI; AC2 TestSoftwareVolumeInExclusive (bit-perfect at 100%, altered at 95%), TestAdjustVolume, TestSetVolumeControl, TestVolumeKeptOnNewEngine, TestVolumeKeys, TestVolumeControls; AC3 TestSettingsVolumeRow, TestFormatLine (vol N%), settings_80x24 golden; AC4 doc-004 plus README. MPRIS: TestVolumeWrites. Mutation checks: 18 mutants, all killed (two survivors fixed by tightening TestVolumeWrites and the playerConfig check).
Verified: go test -race ./... passes; vet and gofmt clean; build ok and -version runs. Coverage: cmd/needle 96.4%, config 96.7%, mpris 96.9%, player 95.9%, tui 99.4%.

Reviewed and approved by the user.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Volume control choice. New [audio] volume = "device" (default, v1 behavior) or "software", also the Exclusive volume row in Settings, applied live and saved. With software, - and = (new keys, 5% steps, with a notice) work in exclusive mode and the indicator shows altered below 100%, bit-perfect at 100%. The bar shows vol N% below 100%. The level starts at 100% each run, survives engine switches while allowed, and resets to 100% when switching back to device. MPRIS Volume writes now work where the keys do (decision 87 replaces 57). ALSA hardware volume evaluated in doc-004: it keeps the USB stream bit-perfect; not built, since no DAC with a mixer is available to test. Risk: the mpv engine no longer refuses volume in exclusive mode itself; the Player enforces it, and the indicator still reads mpv's real volume.
<!-- SECTION:FINAL_SUMMARY:END -->
