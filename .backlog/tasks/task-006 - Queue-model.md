---
id: TASK-006
title: Queue model
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T05:39:41Z'
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
- [x] #1 Play next inserts after the current item; Add to queue appends to the end
- [x] #2 Next and previous move to the adjacent track; previous at the first track restarts it
- [x] #3 Shuffle reorders upcoming items only and can be turned off to restore the original order
- [x] #4 Repeat cycles off, all, and one with the expected next-track behavior for each
- [x] #5 Remove deletes a chosen upcoming item; clear empties upcoming items without stopping the current track
- [x] #6 With repeat off, reaching the end of the queue stops playback without error
- [x] #7 Queue is safe for concurrent access from the player and UI (verified with -race)
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 Every acceptance criterion has at least one test that fails without the change
- [x] #2 go test -race ./..., go vet ./..., and gofmt -l . are clean
- [x] #3 Coverage: changed non-UI packages 95%+, TUI packages 80%+ (teatest/golden); API code has httptest coverage against a fake TIDAL server
- [x] #4 Security self-review done (input validation, no token or internal leaks in errors, shared-state concurrency, no fail-open paths)
- [x] #5 go build ./cmd/tidal-tui succeeds and the binary starts
- [x] #6 README updated for behavior, config, keybinding, or tradeoff changes
- [x] #7 Only dependencies named in the ticket added; no out-of-scope changes
- [x] #8 slog logging to $XDG_STATE_HOME/tidal-tui/ with track_id/request_id fields; no tokens logged
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Package internal/queue, standard library only, generic over the item type so it stays independent of the API and UI.
1. Queue[T] guarded by a mutex. Two orderings of stable entries (each with a uint64 handle so duplicates are allowed): the original order and the play order, plus a cursor into the play order.
2. Set(items, start) replaces the queue; PlayNext inserts after the current entry (in both orderings); Add appends; Remove(handle) removes an upcoming entry; Clear drops upcoming entries but keeps history and the current track.
3. Navigation: Advance (track ended: repeat one replays, repeat all wraps, repeat off returns ok=false and stops), Next (user skip: like Advance, but repeat one moves on), Previous (previous entry; at the first entry it restarts the current one), Jump(handle).
4. Shuffle on reorders only upcoming entries with an injectable RNG; shuffle off restores upcoming entries to their original relative order. Repeat cycles off -> all -> one -> off.
5. Snapshot() returns history, current, upcoming, shuffle, and repeat for the UI.
6. Table-driven tests plus a concurrent stress test under -race.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented 2026-11-22 in internal/queue (standard library only), generic Queue[T].
- Entries have stable handles (duplicates allowed); original and play orderings plus a cursor; mutex on every method.
- Set(items, start), PlayNext, Add, Remove(handle) (upcoming only), Clear (keeps history and current), Advance/Next/Previous/Jump, SetShuffle/Shuffle, SetRepeat/CycleRepeat/Repeat, Len, Snapshot.
- Semantics: Advance under repeat one replays; Next always moves on (repeat one wraps like all); repeat off returns ok=false at the end and leaves the last track current; Previous at the first entry returns the current track to restart; shuffle on shuffles upcoming only, PlayNext still lands right after current; shuffle off restores unplayed entries in original order; repeat all with shuffle reshuffles on wrap.
- Tests: 100% coverage, stable across -count=3 with -race, concurrent stress test with 8 goroutines; 8/8 mutations killed.
- DoD notes: no README change (internal package, no user-visible behavior yet); no logging needed in a pure data structure.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Generic, concurrency-safe play queue in internal/queue: play next, add, remove, clear, next/previous/jump, reversible shuffle of upcoming tracks, repeat off/all/one, stop at the end. 100% coverage, race-clean, 8/8 mutations killed. Reviewed and committed by the user (2d1e1fc).
<!-- SECTION:FINAL_SUMMARY:END -->
