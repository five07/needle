---
id: TASK-007
title: Playback manifest retrieval and quality selection
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T05:52:07Z'
labels:
  - api
  - player
milestone: m-1
dependencies:
  - TASK-005
documentation:
  - doc-001
priority: high
type: feature
ordinal: 7000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Bit-perfect playback starts with requesting the right stream. The quality setting and fallback behavior come from Q6.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A track ID resolves via GET /trackManifests/{id} using the format list mapped from the quality setting: aac=[AACLC], lossless=[FLAC,AACLC], hires=[FLAC_HIRES,FLAC,AACLC]
- [x] #2 The formats actually present in the manifest are reported to the player
- [x] #3 A PREVIEW trackPresentation is surfaced to the user with its previewReason instead of playing silently as a full track
- [x] #4 A manifest carrying drmData is rejected with a clear typed error (see decision-001)
- [x] #5 403 codes CONCURRENT_PLAYBACK, GEO_RESTRICTED, PURCHASE_REQUIRED, and CLIENT_NOT_ENTITLED map to user-readable messages
- [x] #6 The next queue item manifest can be prefetched ahead of track end for gapless playback
- [x] #7 Expired manifest URLs are re-fetched transparently
- [x] #8 httptest covers each quality setting, preview, DRM, and each 403 code
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
T1 internal/tidal/manifests.go: Format constants; Client.TrackManifest(ctx, trackID, formats) calling GET /trackManifests/{id} with manifestType=MPEG_DASH, uriScheme=DATA, usage=PLAYBACK, adaptive=false. Track IDs must be numeric (rejected locally). Returns Manifest{TrackID, Formats, Presentation, PreviewReason, MPD bytes, Hash, track/album ReplayGain, SampleRate and Duration from the MPD, FetchedAt, ExpiresAt}. Decodes the base64 data URI with a size cap. drmData present, or ContentProtection in the MPD -> ErrDRMProtected (decision-001). ExpiresAt = earliest CloudFront DateLessThan in the segment URLs, falling back to FetchedAt+50 min when unparseable. The URI and MPD never appear in logs or errors.
T2 internal/stream (new): FormatsFor(config.Quality) mapping aac/lossless/hires. Resolver over the client: Resolve(ctx, id) returns a cached manifest only if it stays valid for the track duration plus 2 min, otherwise re-fetches; concurrent requests for one ID share a single fetch; Prefetch(ctx, id) warms the cache in the background (errors logged with track_id); Invalidate(id) for the engine after a segment 403; small bounded cache that drops expired entries.
T3 Messages: stream.Message(err) maps CONCURRENT_PLAYBACK, GEO_RESTRICTED, PURCHASE_REQUIRED, CLIENT_NOT_ENTITLED, PREREQUISITE_MISSING, DRM, and login-required to user-readable text; PreviewMessage(reason) for the three previewReason values. README Playback note; doc-001 updated with the expiry finding.
Tests: httptest fake TIDAL per quality (asserting the formats query), preview, DRM (both forms), each 403 code, malformed or oversized data URIs, fake clock for expiry and re-fetch, call-count stubs for dedup, prefetch, and invalidate, -race.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Live check 2026-11-22 (track 330930030, DASH): the manifest URI (Expires param) and every segment URL (CloudFront Policy DateLessThan) expire 60 minutes after issue. The MPD Representation carries audioSamplingRate; id has the form "FLAC_HIRES,192000,24" (undocumented).

Implemented 2026-11-22.
- internal/tidal/manifests.go: Client.TrackManifest (DASH, uriScheme=DATA, usage=PLAYBACK, adaptive=false; numeric IDs only; 1 MB MPD cap). Manifest carries Formats, Presentation/PreviewReason (Preview() is true for anything but FULL), Hash, MPD, ReplayGain, SampleRate, Duration, FetchedAt, ExpiresAt (earliest CloudFront Expires or Policy DateLessThan, fallback +50 min, skew guard). DRM via drmData or ContentProtection -> *DRMError matching ErrDRMProtected. String/GoString/LogValue never include the MPD.
- internal/stream: FormatsFor(quality); Resolver with Resolve (reuse only if valid for duration + 2 min), single shared fetch per track detached from the caller context, Prefetch, Invalidate, cache bounded at 8; logs manifest resolved/failed with track_id. Message(err) and PreviewMessage(reason) for user-facing text.
- Coverage: tidal 99.3%, stream 100%; -race clean over repeated runs; 20/20 mutations killed after tightening three tests.
- Live run: all manifest requests returned 403 CONCURRENT_PLAYBACK because the account was playing elsewhere (mapping confirmed live); bogus ID returned 404 CONTENT_NOT_FOUND. The full success decode path was not run live through the new code; the same request parameters were confirmed live minutes earlier with the expiry probe.
- README Playback section; doc-001 TASK-007 findings appended.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Stream manifest retrieval and quality selection. internal/tidal: Client.TrackManifest requests inline DASH manifests (numeric IDs only, 1 MB cap), reports delivered formats, full vs preview with reason, ReplayGain, sample rate, duration, and signed-URL expiry (earliest CloudFront expiry, 50 min fallback); DRM via drmData or ContentProtection is refused with a typed error; the MPD never reaches logs or errors. internal/stream: quality to formats mapping, Resolver with cache reuse only when valid for the track plus 2 min, one shared fetch per track, Prefetch, Invalidate, bounded cache; Message and PreviewMessage for user-facing text. Coverage tidal 99.3%, stream 100%, race clean, 20/20 mutations killed. Live: CONCURRENT_PLAYBACK mapping confirmed; full success path not re-run live through the new code. Follow-ups: mid-track re-resolve for tracks over ~58 min and bit depth from the decoded stream (TASK-008/013). Reviewed by the user.
<!-- SECTION:FINAL_SUMMARY:END -->
