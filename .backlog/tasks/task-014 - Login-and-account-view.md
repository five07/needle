---
id: TASK-014
title: Login and account view
status: To Do
assignee: []
created_date: '2026-11-22T03:53:36Z'
updated_date: '2026-11-22T04:28:53Z'
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
- [ ] #1 Unauthenticated launch shows a login screen with the authorize URL and waits for the PKCE callback
- [ ] #2 Account view shows username, country, and email from GET /users/me
- [ ] #3 Account view shows the highest quality received so far, since the API exposes no subscription tier
- [ ] #4 Logout from the account view returns to the login screen
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
