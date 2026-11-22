---
id: TASK-019
title: Seek within the current track (scrubbing)
status: To Do
assignee: []
created_date: '2026-11-22T06:54:53Z'
labels:
  - player
  - tui
  - post-mvp
dependencies:
  - TASK-013
priority: low
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md New Items 2: jump within the playing track from the keyboard, with the step growing (5, 10, 30 s) the faster the key is pressed. v1 has no seeking (decision 8), and the left/right arrows switch panes (TASK-009, decision 26), so this needs its own keys. Previews from the current access tier are only 30 s long.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Two keys (not the arrows) seek backward and forward in the current track
- [ ] #2 Repeated presses within a short interval grow the step from 5 s to 10 s to 30 s
- [ ] #3 Seeking never crosses into the previous or next track
- [ ] #4 The now-playing progress updates immediately after a seek
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
