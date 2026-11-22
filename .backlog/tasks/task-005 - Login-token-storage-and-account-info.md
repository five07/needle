---
id: TASK-005
title: Login, token storage, and account info
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T05:33:58Z'
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
- [x] #1 First launch with no stored credentials starts the OAuth authorization code + PKCE flow, opens the browser, and also prints the authorize URL
- [x] #2 The redirect is received on a loopback listener bound to 127.0.0.1 only, the state parameter is verified, and the listener shuts down after one callback or a timeout
- [x] #3 Tokens are stored in the OS keyring (Secret Service); when no keyring is available they fall back to a 0600 file under $XDG_CONFIG_HOME/tidal-tui/ and a warning is logged
- [x] #4 Only one account is stored at a time; logging in again replaces it
- [x] #5 Expired access tokens refresh silently; a revoked refresh token returns the user to login
- [x] #6 Logout removes stored credentials from whichever store holds them
- [x] #7 Account info from GET /users/me (username, country, email) is available to the UI
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
1. internal/tidal/users.go: Client.Me(ctx) -> User{ID, Username, Country, Email, EmailVerified} from GET /users/me.
2. internal/auth/oauth.go: PKCE login (S256 challenge, random state) with a loopback listener bound to 127.0.0.1 only; redirect URI derived from the bound address; one callback or timeout, then shutdown; code exchange and refresh against auth.tidal.com with client_id only (no secret). OAuth errors map to typed errors; invalid_grant on refresh -> ErrLoginRequired.
3. internal/auth/store.go: Store interface (Load/Save/Delete, ErrNoCredentials). KeyringStore (service tidal-tui) storing JSON; FileStore at $XDG_CONFIG_HOME/tidal-tui/credentials.json with 0700 dir and 0600 file; NewStore probes the keyring and falls back to the file with a logged warning; Delete clears both.
4. internal/auth/tokensource.go: implements tidal.TokenSource; caches tokens in memory, refreshes when expired (60 s skew) or on demand, persists rotated tokens, serializes refreshes with a mutex; a revoked refresh token deletes stored credentials and returns ErrLoginRequired.
5. cmd/tidal-tui: until the TUI exists (TASK-014), a launch without credentials runs the login in the terminal (prints the URL, opens the browser); -logout removes stored credentials; after login it fetches /users/me and prints the username. A missing client ID is a clear error.
6. Tests: fake authorize/token server with a simulated browser following the redirect, keyring mock (go-keyring MockInit/MockInitWithError) for available/unavailable paths, file store permissions, token source refresh/rotation/revocation/concurrency, Me() via httptest, main wiring via injected seams.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
User decision 2026-11-22: keyring via github.com/zalando/go-keyring (approved dependency).

Implemented 2026-11-22.
- internal/tidal/users.go: Client.Me -> User{ID, Username, Country, Email, EmailVerified}.
- internal/auth: OAuth (PKCE S256, loopback 127.0.0.1 only, constant-time state check, stray callbacks ignored, 5 min timeout, shutdown after one callback; client_id only; invalid_grant -> ErrLoginRequired; bounded error text, url.Error unwrapped). Store: KeyringStore (go-keyring, service tidal-tui) and FileStore (0700 dir, 0600 file via temp+rename, refuses group/other-readable files); NewStore probes the keyring and falls back with a WARN log; DeleteAll clears both. TokenSource: cached, 60 s expiry skew, serialized refresh, keeps the non-rotated refresh token, deletes credentials and returns ErrLoginRequired on revocation.
- cmd/tidal-tui: -logout flag; missing client ID exits 1 with guidance; no credentials -> terminal login; revoked -> one re-login; prints "Logged in as <username> (<country>)". ctx from signal.NotifyContext so Ctrl-C aborts login.
- Coverage: auth 96.8% (uncovered: json.Marshal on a fixed struct, temp file write/close failures, xdg-open), tidal 99.2%, cmd 98.9%. Mutations: auth 16/16, main 4/4 killed. Tests assert no token appears in logs or terminal output.
- Not exercised live against TIDAL in this change; the same flow was proven live by the TASK-001 spike. User to verify: go run ./cmd/tidal-tui with TIDAL_CLIENT_ID set.
- New deps: github.com/zalando/go-keyring v0.2.8 (approved), indirect godbus/dbus/v5, danieljoos/wincred, golang.org/x/sys.
<!-- SECTION:NOTES:END -->
