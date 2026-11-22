---
id: TASK-004
title: TIDAL API client core
status: To Do
assignee: []
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T04:28:52Z'
labels:
  - api
milestone: m-0
dependencies:
  - TASK-001
  - TASK-002
documentation:
  - doc-001
  - decision-001
priority: high
type: feature
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
All catalog, library, and playback features share transport concerns: auth headers, JSON:API decoding, cursor pagination, rate limits, and error mapping. The client targets the official API only (decision-001, doc-001).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Requests carry context.Context and the current access token
- [ ] #2 JSON:API documents (application/vnd.api+json) decode with included resources resolved by type and id
- [ ] #3 HTTP 401 triggers one token refresh and retry; a second 401 returns a typed auth error
- [ ] #4 HTTP 429 retries with bounded exponential backoff, honoring Retry-After when present (the spec documents no limits or header)
- [ ] #5 Cursor pagination (page[cursor], links.next) is exposed as an iterator that stops on context cancel
- [ ] #6 JSON:API errors[] map to typed errors callers can branch on (for example CONCURRENT_PLAYBACK, GEO_RESTRICTED)
- [ ] #7 Mutating requests send an Idempotency-Key header
- [ ] #8 Error messages never include tokens or full response bodies
- [ ] #9 httptest fake server covers success, 401, 429, 5xx, JSON:API errors, and malformed JSON
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
