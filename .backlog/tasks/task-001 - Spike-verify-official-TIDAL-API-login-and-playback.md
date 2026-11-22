---
id: TASK-001
title: 'Spike: verify official TIDAL API login and playback'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T04:54:40Z'
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
- [x] #1 A registered developer app completes PKCE login with scopes playback, user.read, collection.read, playlists.read, recommendations.read, search.read, and the granted scopes (including whether r_usr is granted) are recorded
- [x] #2 Whether a loopback redirect URI (http://127.0.0.1:<port>/callback) is accepted at app registration and login is recorded
- [x] #3 A trackManifests request with formats FLAC_HIRES,FLAC for a known hi-res track is made and trackPresentation, previewReason, formats, and drmData presence are recorded
- [x] #4 If the manifest is full and DRM-free, it is confirmed playable (or not) by mpv and the bit depth and sample rate mpv reports are recorded
- [x] #5 Token lifetime and refresh token behavior are recorded
- [x] #6 Findings are recorded in doc-001, and any blocker (DRM, previews, missing scope) is written up with the next step for raising it with TIDAL
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a minimal go.mod (module github.com/five07/tidal-tui) so the spike can build; TASK-002 owns the rest of the scaffold.
2. Write a throwaway stdlib-only CLI at cmd/tidal-spike with subcommands: login (PKCE + loopback 127.0.0.1:8765), refresh, whoami (/users/me), get <path> (raw GET for probing r_usr-scoped endpoints), manifest <id> (trackManifests + MPD summary), play <id> (hand manifest to mpv for N seconds).
3. Tokens saved to a 0600 file in the session scratchpad via TIDAL_SPIKE_TOKENS; token values and signed URLs are never printed.
4. User runs login interactively; Claude runs the other probes against tracks 330930030 and 317688868.
5. Record results in doc-001; write up any blocker (DRM, preview, missing r_usr) per decision-001.
6. Delete or keep the spike tool per user preference after findings are recorded.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Spike run 2026-11-21 with cmd/tidal-spike against the registered THIRD_PARTY app.
- PKCE + loopback 127.0.0.1:8765 works; all 10 dashboard scopes granted, no r_usr. Token 14400 s, refresh works, no rotation.
- All r_usr-listed endpoints probed (users/me, searchResults, userCollection*, playlists, user*Mixes, trackManifests) return 200 without r_usr.
- trackManifests: every format/type combination returns PREVIEW (30 s) with previewReason FULL_REQUIRES_HIGHER_ACCESS_TIER. drmData absent, no ContentProtection. EAC3_JOC returns 403.
- mpv plays the preview MPD (flac 2ch 192000 Hz, AO s32 192 kHz on --ao=null) once FFmpeg protocol_whitelist allows https for a local MPD file.
- Full details in doc-001 "TASK-001 spike results".

User review 2026-11-21: keep cmd/tidal-spike as a debugging tool until TASK-004/TASK-007 replace it. Development proceeds against 30 s previews while the user pursues full-track access with TIDAL.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Verified the official TIDAL API with a real registered THIRD_PARTY app. Login (PKCE, loopback redirect), every user-scoped endpoint, and DRM-free hi-res FLAC DASH manifests all work, and mpv decodes them at the source format (24/192). r_usr is not required in practice.

Blocker: the THIRD_PARTY tier only receives 30-second previews (FULL_REQUIRES_HIGHER_ACCESS_TIER). Full tracks need a higher developer tier, probably THIRD_PARTY_PROD; the user is asking TIDAL how to obtain it (decision-001). Development proceeds against previews.

The spike tool cmd/tidal-spike is kept as a debugging aid until TASK-004/TASK-007 replace it. Hardware playback in exclusive ALSA mode belongs to TASK-008.
<!-- SECTION:FINAL_SUMMARY:END -->
