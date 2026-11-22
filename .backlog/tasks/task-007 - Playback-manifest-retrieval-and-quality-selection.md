---
id: TASK-007
title: Playback manifest retrieval and quality selection
status: To Do
assignee: []
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T04:28:53Z'
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
- [ ] #1 A track ID resolves via GET /trackManifests/{id} using the format list mapped from the quality setting: aac=[AACLC], lossless=[FLAC,AACLC], hires=[FLAC_HIRES,FLAC,AACLC]
- [ ] #2 The formats actually present in the manifest are reported to the player
- [ ] #3 A PREVIEW trackPresentation is surfaced to the user with its previewReason instead of playing silently as a full track
- [ ] #4 A manifest carrying drmData is rejected with a clear typed error (see decision-001)
- [ ] #5 403 codes CONCURRENT_PLAYBACK, GEO_RESTRICTED, PURCHASE_REQUIRED, and CLIENT_NOT_ENTITLED map to user-readable messages
- [ ] #6 The next queue item manifest can be prefetched ahead of track end for gapless playback
- [ ] #7 Expired manifest URLs are re-fetched transparently
- [ ] #8 httptest covers each quality setting, preview, DRM, and each 403 code
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
