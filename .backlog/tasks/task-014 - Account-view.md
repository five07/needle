---
id: TASK-014
title: Account view
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:36Z'
updated_date: '2026-11-22T17:46:12Z'
labels:
  - tui
  - auth
milestone: m-2
dependencies:
  - TASK-005
  - TASK-009
priority: medium
type: feature
ordinal: 14000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The login flow and account info from task-005 need a UI surface.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Account view shows username, country, and email from GET /users/me
- [x] #2 Account view shows the highest quality received so far, since the API exposes no subscription tier
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
1. T1: Account view: username, country, email; quality, audio mode, device (decision 53).
2. T2: highest quality received: ranked by sample rate, bit depth, lossless; remembered in $XDG_STATE_HOME/tidal-tui/best_quality.json with the date first seen; previews ignored; -logout deletes it (decision 52).
3. T3: README, DoD, mutation checks, report.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Scope simplified by the user (2026-11-22): login stays in the terminal before the TUI starts, as it already works (OAuth runs when there are no stored credentials, then the TUI opens), and logout stays the -logout flag. The former AC #1 (login screen) and AC #4 (logout from the Account view) were removed. Remaining: the Account view.

T1-T3 implemented. AccountView shows username, country, email (marked when not verified), the highest quality received with the date first seen, and the read-only quality, audio mode, and device (decision 53). The best quality is tracked in the status watcher from player status (sample rate, then bit depth, then lossless; previews ignored), sent with each NowPlaying snapshot, and saved atomically to $XDG_STATE_HOME/tidal-tui/best_quality.json; damaged files are ignored; -logout deletes it (decision 52). Coverage: tui 99.4%, cmd 96.1%. 15 mutations: 14 killed after three test fixes, 1 equivalent (> vs >= inside a != branch). Awaiting user review.

User reviewed and approved.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
The Account view replaces the placeholder. It shows the username, country, and email from GET /users/me (the email marked when not verified), the highest quality TIDAL has delivered with the date first seen, and the read-only quality, audio mode, and device settings. The best quality is ranked by sample rate, then bit depth, then lossless; previews are ignored; it is saved atomically to $XDG_STATE_HOME/tidal-tui/best_quality.json and deleted by -logout. Login and logout stay in the terminal (scope simplified by the user). Verified: go test -race, vet and gofmt clean; tui coverage 99.4%, cmd 96.1%; 14 of 15 mutations caught, the survivor is equivalent. Follow-up: editable audio settings as a post-MVP task.
<!-- SECTION:FINAL_SUMMARY:END -->
