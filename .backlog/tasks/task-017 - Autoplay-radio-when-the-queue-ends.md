---
id: TASK-017
title: Autoplay radio when the queue ends
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T04:28:52Z'
updated_date: '2026-11-22T19:09:43Z'
labels:
  - player
  - api
dependencies:
  - TASK-006
  - TASK-012
priority: low
type: enhancement
ordinal: 17000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Deferred from v1 by PLAN.md Q11. With repeat off, playback stops at the end of the queue; some users want it to continue with related music.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 An autoplay setting (default off) continues with track radio for the last played track when the queue ends
- [x] #2 Autoplayed tracks are marked as such in the queue view
- [x] #3 Turning autoplay off mid-radio stops after the current track
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
1. T1: player.Track.Autoplay; queue AddBefore and RemoveUpcoming; player Status.EndOfQueue.
2. T2: config autoplay; cmd autoplay component in the status watcher (fetch when the last track plays or ran out, append first radio page minus queued tracks, continue on EndOfQueue); playback SetAutoplay/Add ahead of autoplayed; saved mark.
3. T3: TUI c key, bar ∞ toggle, Queue ∞ mark and legend, Account line.
4. T4: README, DoD, mutations, manual check.
5. T5: notes and report.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
T1-T4 implemented. config: autoplay (bool, startup value). queue: AddBefore, RemoveUpcoming. player: Track.Autoplay, Status.EndOfQueue (set when the queue runs out, by next on the last track, or by a skip at the end; cleared by load and stop). cmd/needle/autoplay.go: check() on the watcher starts one fetch (TrackRadio + first PlaylistTracks page, 30 s timeout) when the last track plays or ran out, autoplay on, repeat off; apply() appends new tracks marked autoplayed, drops results from before a switch change, notices for no radio / failure / nothing new, and calls Next when playback ran out on the seed. A failure is not retried for the same seed until the switch is used. playback: SetAutoplay (off removes upcoming autoplayed), Add inserts ahead of autoplayed. MPRIS CanGoNext with autoplay. TUI: c key, ∞ on/off bar control (click), Queue ∞ mark (plain text, deviation from dim) and leading ∞ autoplay legend, Account 'Autoplay on/off at start' under a 'Settings' heading. Saved queue keeps the mark. TASK-027 got an AC for editing autoplay in Account. Coverage: queue 100%, player 95.8%, tui 99.4%, config 97.6%, cmd 96.3%. 51 mutations; all killed after test fixes. Awaiting user review.

User tested and approved.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Autoplay continues the queue with TIDAL track radio when it runs out. autoplay = false in config.toml sets it at startup; c or a click on the bar's ∞ control toggles it for the session; the Account view shows the startup value under Settings. With autoplay on and repeat off, when the last track plays (or the queue just ran out, including next on the last track) needle fetches that track's radio and appends the first page as autoplayed entries, leaving out tracks already queued, so playback continues gaplessly and drifts from each new last track. Autoplayed rows show ∞ in the Queue view with a leading '∞ autoplay' legend; a inserts your tracks ahead of them; turning autoplay off removes the upcoming autoplayed tracks and lets the current one finish. No radio, a failed fetch, or nothing new gives a notice and is not retried for that track. The mark is kept in the saved queue; MPRIS reports CanGoNext with autoplay on. Decisions 62-66; editing autoplay in Account is part of TASK-027. Verified: go test -race, vet and gofmt clean; queue 100%, player 95.8%, tui 99.4%, config 97.6%, cmd 96.3%; 51 mutations all caught; manually tested by the user.
<!-- SECTION:FINAL_SUMMARY:END -->
