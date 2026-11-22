---
id: TASK-001
title: 'Spike: verify official TIDAL API login and playback'
status: To Do
assignee: []
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T04:19:17Z'
labels:
  - api
  - research
milestone: m-0
dependencies: []
references:
  - https://tidal-music.github.io/tidal-api-reference/tidal-api-oas.json
  - https://developer.tidal.com/documentation/api-sdk/api-sdk-overview
documentation:
  - doc-001
  - doc-002
priority: high
type: spike
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
tidal-tui uses only the official TIDAL API (decision-001). The spec (doc-001) gives THIRD_PARTY apps GET /trackManifests/{id} with FLAC_HIRES, but the response may carry Widevine/FairPlay drmData, may serve a PREVIEW with previewReason FULL_REQUIRES_HIGHER_ACCESS_TIER, and requires scope r_usr, which the spec marks INTERNAL. Login is PKCE only. Every playback and auth ticket depends on what actually happens with a real registered app. If DRM or previews block playback, the outcome is a documented blocker to raise with TIDAL, not a workaround.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A registered developer app completes PKCE login with scopes playback, user.read, collection.read, playlists.read, recommendations.read, search.read, and the granted scopes (including whether r_usr is granted) are recorded
- [ ] #2 Whether a loopback redirect URI (http://127.0.0.1:<port>/callback) is accepted at app registration and login is recorded
- [ ] #3 A trackManifests request with formats FLAC_HIRES,FLAC for a known hi-res track is made and trackPresentation, previewReason, formats, and drmData presence are recorded
- [ ] #4 If the manifest is full and DRM-free, it is confirmed playable (or not) by mpv and the bit depth and sample rate mpv reports are recorded
- [ ] #5 Token lifetime and refresh token behavior are recorded
- [ ] #6 Findings are recorded in doc-001, and any blocker (DRM, previews, missing scope) is written up with the next step for raising it with TIDAL
<!-- AC:END -->
