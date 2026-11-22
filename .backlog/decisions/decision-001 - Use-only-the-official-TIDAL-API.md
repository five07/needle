---
updated_date: '2026-11-22T04:54:40Z'
id: decision-001
title: Use only the official TIDAL API
date: '2026-11-22T04:19:05Z'
status: accepted
---
## Context

<!-- SECTION:CONTEXT:BEGIN -->
tidal-tui must be a legal, legitimate client. The official TIDAL API (openapi.tidal.com/v2) offers THIRD_PARTY apps catalog, search, library, mixes, and GET /trackManifests/{id} for playback (doc-001). Its manifests may carry Widevine/FairPlay DRM or serve previews, which is unverified.
The alternative, the private v1 API (api.tidal.com/v1) with a client ID and secret taken from TIDAL's own app, is what existing clients such as tidalt use (doc-002). It avoids DRM but relies on borrowed credentials and breaks TIDAL's terms.
<!-- SECTION:CONTEXT:END -->

## Decision

<!-- SECTION:DECISION:BEGIN -->
Use only the official TIDAL API, with our own registered app and OAuth authorization code + PKCE. Never use the private v1 API, borrowed client IDs or secrets, or any DRM circumvention.
DRM is handled when we hit it: if manifests turn out to be encrypted, we decide then how to proceed within TIDAL's terms (for example by asking TIDAL about access tiers or a licensed decryption path) rather than working around it.
<!-- SECTION:DECISION:END -->

## Consequences

<!-- SECTION:CONSEQUENCES:BEGIN -->
- Login is browser-based PKCE with a loopback redirect. Verified in TASK-001: http://127.0.0.1:8765/callback works with no client secret.
- Features limited to what THIRD_PARTY access exposes: no home feed, genres, editorial pages, library text search, or server-side play queue. r_usr is not granted but is not needed in practice (TASK-001).
- No need for an abstraction layer to swap API backends. The API client targets the official API directly.
- The UI must report preview-only, geo, purchase, and concurrency restrictions clearly instead of failing silently.

### TASK-001 outcome (2026-11-21): the blocker is access tier, not DRM

Manifests for the THIRD_PARTY tier are DRM-free (no drmData, no ContentProtection) hi-res FLAC DASH that mpv plays at the source format, but every track is served as a 30-second PREVIEW with previewReason FULL_REQUIRES_HIGHER_ACCESS_TIER. Full tracks need a higher developer tier, probably THIRD_PARTY_PROD (unconfirmed).

- The project owner is asking TIDAL how to obtain that tier and what it requires for an open-source client (review, branding, publishing the client ID).
- Development continues against 30-second previews, which exercise the full pipeline: manifests, DASH, hi-res FLAC, mpv, and bit-perfect output.
- If TIDAL declines full-track access, the fallback options below still apply, with option 3 (browse-and-control client handing playback to an official TIDAL app) the most likely.

### If manifests ever arrive DRM-encrypted

Widevine/FairPlay content can only be decrypted legitimately by a licensed CDM (Chrome/Firefox EME, Android, Apple platforms). Open-source decryptors (pywidevine and Go ports) rely on keys extracted from real devices and are circumvention, so they are ruled out. Server-side Widevine libraries such as github.com/alfg/widevine (a Widevine Cloud client for content owners) do not help a player. Legitimate options, in order of preference:

1. Ask TIDAL whether third-party apps can receive DRM-free FLAC manifests, or whether a higher access tier does.
2. Play through a licensed browser CDM (EME). Legitimate, but breaks the terminal-only model and gives up bit-perfect output.
3. Reduce scope to a browse-and-control client: search, library, and queue in the terminal, with playback handed off to an official TIDAL app.
<!-- SECTION:CONSEQUENCES:END -->

## Alternatives

<!-- SECTION:ALTERNATIVES:BEGIN -->
- Private v1 API with borrowed app credentials (rejected: not legitimate, ToS risk, breaks without notice).
- Official API with private-API fallback for playback only (rejected for the same reason).
<!-- SECTION:ALTERNATIVES:END -->

