---
id: TASK-006
title: Queue model
status: To Do
assignee: []
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T04:28:53Z'
labels:
  - player
milestone: m-1
dependencies:
  - TASK-002
priority: high
type: feature
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md requires Play next and Add to queue, plus advance/rewind, decided as previous/next track only with no seeking (Q8). Queue extras decided in Q11. A pure, UI-independent queue model keeps this logic testable apart from audio and the TUI. The queue is local only: TIDAL play queues are INTERNAL (doc-001).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Play next inserts after the current item; Add to queue appends to the end
- [ ] #2 Next and previous move to the adjacent track; previous at the first track restarts it
- [ ] #3 Shuffle reorders upcoming items only and can be turned off to restore the original order
- [ ] #4 Repeat cycles off, all, and one with the expected next-track behavior for each
- [ ] #5 Remove deletes a chosen upcoming item; clear empties upcoming items without stopping the current track
- [ ] #6 With repeat off, reaching the end of the queue stops playback without error
- [ ] #7 Queue is safe for concurrent access from the player and UI (verified with -race)
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
