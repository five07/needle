---
id: TASK-004
title: TIDAL API client core
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T05:39:41Z'
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
- [x] #1 Requests carry context.Context and the current access token
- [x] #2 JSON:API documents (application/vnd.api+json) decode with included resources resolved by type and id
- [x] #3 HTTP 401 triggers one token refresh and retry; a second 401 returns a typed auth error
- [x] #4 HTTP 429 retries with bounded exponential backoff, honoring Retry-After when present (the spec documents no limits or header)
- [x] #5 Cursor pagination (page[cursor], links.next) is exposed as an iterator that stops on context cancel
- [x] #6 JSON:API errors[] map to typed errors callers can branch on (for example CONCURRENT_PLAYBACK, GEO_RESTRICTED)
- [x] #7 Mutating requests send an Idempotency-Key header
- [x] #8 Error messages never include tokens or full response bodies
- [x] #9 httptest fake server covers success, 401, 429, 5xx, JSON:API errors, and malformed JSON
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
Package internal/tidal, standard library only.
1. TokenSource interface (Token, Refresh) so TASK-005 can supply tokens; Client with options (base URL, HTTP client, user agent, logger, retry policy, sleep and idempotency-key seams for tests).
2. Client.Do/Get/Post/Patch/Delete: context on every request, Bearer token, Accept/Content-Type application/vnd.api+json, body marshaled once and re-sent on retry, Idempotency-Key generated once per logical mutating request and reused across retries.
3. Retry policy: 401 -> one Refresh then retry, second 401 -> ErrUnauthorized; 429 and 502/503/504 -> bounded exponential backoff (default 3 retries, 500 ms base, 30 s cap) honoring Retry-After seconds or HTTP-date; Retry-After beyond the cap returns the error immediately with RetryAfter set; backoff sleep observes ctx.
4. JSON:API types: Document (data single or array, included, links, meta), Resource, Relationship, Identifier, Links (string or object values, links.meta ignored); helpers to get single/many resources, find included by type+id, resolve relationship linkage, decode attributes and meta generically.
5. Errors: *APIError{Status, Code, Detail, Errors} with errors.Is mapping to sentinels (ErrUnauthorized, ErrNotFound, ErrRateLimited, ErrConcurrentPlayback, ErrGeoRestricted, ErrPurchaseRequired, ErrClientNotEntitled, ErrPrerequisiteMissing, ErrInvalidResourceID). Messages carry method, path, status, code, and detail truncated to 200 chars; never tokens or bodies.
6. Pages(ctx, path, query) iter.Seq2[*Document, error] following links.next (relative to the base URL, observed live); absolute next URLs on another host are rejected so tokens never leak; stops on ctx cancel or consumer break.
7. slog debug logging per attempt with request_id, method, path, status, attempt, duration.
8. Table-driven httptest tests with a stub TokenSource that counts calls: success, 401 paths, 429 (seconds, date, none, exhausted, over cap), 5xx, JSON:API errors, malformed JSON, idempotency keys, pagination (multi-page, cancel, break, foreign host, mid-stream error), and document helpers.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Live API observations used for the design (2026-11-22): links.next is a path relative to /v2 with page%5Bcursor%5D, links also carries meta.nextCursor; page size 20; collection items carry meta.addedAt; error objects have code, detail, optional status (string) and source.pointer; an invalid ID returns 400 INVALID_RESOURCE_ID (not 404); EAC3_JOC manifest returns 403 CLIENT_NOT_ENTITLED; responses carry no rate limit headers.

Implemented 2026-11-22 in internal/tidal (standard library only).
- client.go: TokenSource interface, Client with options, Do/Get, retry loop (one refresh on 401; 429/502/503/504 with exponential backoff 500 ms doubling, 3 retries, 30 s cap; Retry-After seconds or HTTP-date; over-cap Retry-After returns at once with RetryAfter set), Idempotency-Key once per logical mutating request, foreign-host guard on absolute paths, slog debug record per attempt with request_id.
- jsonapi.go: Document/Resource/Relationship/Identifier/Links (string or object links, links.meta ignored), One/Many/Find/Resolve/Identifiers, generic DecodeAttributes/DecodeMeta.
- errors.go: *APIError with Code(), errors.Is mapping by code then status to ErrUnauthorized, ErrNotFound, ErrRateLimited, ErrInvalidResourceID, ErrConcurrentPlayback, ErrGeoRestricted, ErrPurchaseRequired, ErrClientNotEntitled, ErrPrerequisiteMissing. Messages carry method, path, status, code, and detail truncated to 200 runes with control characters stripped.
- pages.go: Pages iter.Seq2 following links.next; stops on error, ctx cancel, or break.
- Tests: httptest fake API + counting stub TokenSource; 99.2% coverage; 13/13 mutations killed.
- Live check (temporary build-tagged test, deleted afterwards): album include resolution (1 artist, 14 tracks), 3 real cursor pages (60 items), Atmos manifest -> ErrClientNotEntitled, /albums/0 -> ErrInvalidResourceID.
- Assumption: 500 is not retried (only 502/503/504/429). DoD #6 README: no user-visible behavior change, nothing to document.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Official TIDAL API client core in internal/tidal: JSON:API document model, typed and sentinel errors, one refresh on 401, backoff for 429/502/503/504 with Retry-After, idempotency keys for writes, cursor pagination iterator, host guard, body caps. 99.2% coverage, 13/13 mutations killed. Reviewed and committed by the user (f918157).
<!-- SECTION:FINAL_SUMMARY:END -->
