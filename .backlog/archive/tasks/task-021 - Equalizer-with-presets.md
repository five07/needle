---
id: TASK-021
title: Equalizer with presets
status: To Do
assignee:
  - '@claude'
created_date: '2026-11-22T06:54:53Z'
updated_date: '2026-11-22T20:54:20Z'
labels:
  - player
  - post-mvp
dependencies:
  - TASK-013
priority: low
ordinal: 21000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md New Items 4: an equalizer with presets. Any EQ changes the samples, so it conflicts with bit-perfect output (decision 16): the indicator must show altered whenever EQ is active.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The user can enable an EQ and choose from presets
- [ ] #2 With EQ active the bit-perfect indicator shows altered with the reason
- [ ] #3 EQ off restores the unaltered audio path
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Won't do for now: deferred by the user on 2026-11-22, to be picked up later. Draft questions 96-100 are in PLAN.md under TASK-021.
<!-- SECTION:NOTES:END -->
