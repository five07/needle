---
id: TASK-016
title: Persist the play queue across restarts
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T04:28:52Z'
updated_date: '2026-11-22T18:48:57Z'
labels:
  - player
dependencies:
  - TASK-006
priority: low
type: enhancement
ordinal: 16000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Deferred from v1 by PLAN.md Q11. Restarting the app currently loses the queue and position in it, so users have to rebuild it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Queue contents, current index, shuffle, and repeat state are restored on next launch
- [x] #2 Tracks that are no longer available are skipped with a notice
- [x] #3 Queue state is stored under $XDG_STATE_HOME/needle/
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
1. T1: queue Export/Restore and a revision counter.
2. T2: saved queue file (cmd/needle/savedqueue.go): load and validate at startup, save from watchStatus when the revision changes, -logout deletes it; open on the Queue view when restored; space starts a stopped track.
3. T3: player skips unavailable tracks (not found, invalid ID, geo-restricted) with a notice; stops after a full pass.
4. T4: README, DoD, mutations, manual check.
5. T5: notes and report.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
T1-T4 implemented. queue: Revision counter, Export/Restore (validated: Play a permutation, Current in range, known Repeat). cmd/needle/savedqueue.go: $XDG_STATE_HOME/needle/queue.json (0600, atomic), versioned JSON, load limits (8 MiB, 10,000 tracks, numeric IDs up to 20 digits, 1 KiB fields, durations 0-24 h); damaged files logged and ignored; saved from watchStatus when the revision changes, plus a final save on shutdown (stop now waits for the watcher); a queue over the limit is not saved and the old file removed; -logout deletes it. Restored queue opens the TUI on the Queue view, focused (tui.Config.OpenQueue); space (playback.TogglePause) and MPRIS PlayPause start a stopped track. player: not-found, invalid-ID, and geo-restricted load errors skip to the next track with a notice (Status.Notice/NoticeID, a counted message for a run of skips); stops after a full pass; count resets on start, Play, Next, Previous. Other errors still stop. Coverage: queue 100%, player 95.7%, tui 99.4%, cmd 96.1%. 37 mutations: all killed but one equivalent. Awaiting user review.

User tested and approved.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
The play queue is kept across restarts in $XDG_STATE_HOME/needle/queue.json (0600, atomic writes): history, current track, upcoming tracks in their shuffled order, shuffle, and repeat. It is saved after every queue change (never on position updates) and once more at shutdown; stop() waits for that save. Loading validates the file (size, track count, numeric IDs, field lengths, durations, version, play order); a damaged file is logged and ignored. A restored queue opens the TUI on the Queue view, focused, with the current track selected and stopped; space, enter, or the play button starts it (space now starts any stopped track). -logout deletes the file. In any queue, a track gone from TIDAL or not available in the country is skipped with a notice, and playback stops after a full pass of failures. Decisions 58-61. Verified: go test -race, vet and gofmt clean; queue 100%, player 95.7%, tui 99.4%, cmd 96.1%; 37 mutations, all caught but one equivalent; manually tested by the user.
<!-- SECTION:FINAL_SUMMARY:END -->
