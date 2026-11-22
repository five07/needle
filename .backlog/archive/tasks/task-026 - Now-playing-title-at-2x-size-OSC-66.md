---
id: TASK-026
title: Now-playing title at 2x size (OSC 66)
status: To Do
assignee: []
created_date: '2026-11-22T16:37:17Z'
updated_date: '2026-11-22T20:52:15Z'
labels:
  - tui
dependencies:
  - TASK-013
priority: low
ordinal: 26000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Decision 47: TASK-013 draws the title bold on 2 rows. Charm's cell renderer drops OSC 66, so the 2x title must be written outside the frame. Start with a spike in kitty to check for flicker and repaint problems.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A spike in kitty shows whether an out-of-frame OSC 66 title survives redraws without flicker
- [ ] #2 When supported and the spike passes, the title is drawn at 2x; other terminals keep the bold fallback
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
Won't do: canceled by the user on 2026-11-22.
<!-- SECTION:NOTES:END -->
