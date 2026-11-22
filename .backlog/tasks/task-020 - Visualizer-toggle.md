---
id: TASK-020
title: Visualizer toggle
status: To Do
assignee: []
created_date: '2026-11-22T06:54:53Z'
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
- [ ] #1 A key toggles the visualizer on and off
- [ ] #2 Turning the visualizer on does not change the bit-perfect indicator state
- [ ] #3 The layout stays usable at 80x24 with the visualizer on
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
