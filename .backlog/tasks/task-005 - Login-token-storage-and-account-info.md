---
id: TASK-005
title: Login, token storage, and account info
status: To Do
assignee: []
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T04:28:52Z'
labels:
  - api
  - auth
milestone: m-0
dependencies:
  - TASK-003
  - TASK-004
documentation:
  - doc-001
priority: high
type: feature
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md requires user login and account info storage. Flow and storage depend on Q1, Q2, and Q7.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 First launch with no stored credentials starts the OAuth authorization code + PKCE flow, opens the browser, and also prints the authorize URL
- [ ] #2 The redirect is received on a loopback listener bound to 127.0.0.1 only, the state parameter is verified, and the listener shuts down after one callback or a timeout
- [ ] #3 Tokens are stored in the OS keyring (Secret Service); when no keyring is available they fall back to a 0600 file under $XDG_CONFIG_HOME/tidal-tui/ and a warning is logged
- [ ] #4 Only one account is stored at a time; logging in again replaces it
- [ ] #5 Expired access tokens refresh silently; a revoked refresh token returns the user to login
- [ ] #6 Logout removes stored credentials from whichever store holds them
- [ ] #7 Account info from GET /users/me (username, country, email) is available to the UI
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
