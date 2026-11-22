---
id: doc-001
title: TIDAL API third-party access reference
type: specification
created_date: '2026-11-22T04:07:57Z'
updated_date: '2026-11-22T04:52:09Z'
tags:
  - api
  - tidal
  - reference
---
# TIDAL API: third-party access reference

Source: TIDAL OpenAPI spec `tidal-api-oas.json` version 1.10.258, read 2026-11-21 from
https://tidal-music.github.io/tidal-api-reference/tidal-api-oas.json
(the viewer at https://tidal-music.github.io/tidal-api-reference/?accessTier=THIRD_PARTY renders this spec).

The developer portal (https://developer.tidal.com/documentation/api-sdk/api-sdk-overview) is rendered with JavaScript and returned no text to automated fetching. Anything below that would normally come from the portal (app registration, terms, rate limit numbers) is **not yet verified** and is called out as such.

## Access tiers

- Every operation carries `x-path-item-properties.required-access-tier`: `THIRD_PARTY`, `PARTNER`, or `INTERNAL`.
- The `/users` schema lists the developer tiers `THIRD_PARTY`, `THIRD_PARTY_PROD`, `PARTNER`, `PARTNER_TIDAL_CONNECT`, `INTERNAL`.
- Scopes carry `x-scopes-required-access-tier`. All named scopes are THIRD_PARTY except `r_usr` and `w_usr`, which are marked `INTERNAL`.
- **Open issue:** most user-scoped THIRD_PARTY operations list `r_usr` or `w_usr` as a required scope alongside the named scope (for example `playback` + `r_usr`). How a THIRD_PARTY app satisfies an INTERNAL-tier scope is not explained in the spec. TASK-001 must test this.
- `THIRD_PARTY` vs `THIRD_PARTY_PROD` is not explained. It likely separates development apps from apps approved for production. Unverified.

## Conventions

- Base URL: `https://openapi.tidal.com/v2`. JSON:API, content type `application/vnd.api+json`.
- `include=` with dot-separated relationship paths. `includeLinkage=` returns linkage only.
- Pagination is cursor-only: `page[cursor]`, follow `links.next` until absent. `meta.total` may be approximate.
- `countryCode` (ISO 3166-1 alpha-2) on most catalog reads. `locale` (BCP 47) on collection and mix endpoints.
- `me` is accepted in place of a user ID (`/users/me`, `/userCollectionTracks/me`, `filter[owners.id]=me`).
- Mutations accept an `Idempotency-Key` header (replays within one hour return the stored response; a reused key with a different payload returns 422).
- Errors are JSON:API `errors[]` with `code`, `detail`, `status`.
- Rate limiting: 429 "Rate limit exceeded" is documented on every operation. No numeric limits and no `Retry-After` behavior are documented.

## Authorization

| Scheme | Flow | Endpoints |
|---|---|---|
| `Authorization_Code_PKCE` | OAuth 2 authorization code + PKCE | authorize `https://login.tidal.com/authorize`, token `https://auth.tidal.com/v1/oauth2/token` |
| `Client_Credentials` | OAuth 2 client credentials, no scopes | token `https://auth.tidal.com/v1/oauth2/token` |

- **No device-code flow.** A terminal client must use PKCE, which requires a browser and a redirect URI. Whether a loopback redirect (`http://127.0.0.1:<port>/callback`) is allowed is not verified (portal page unreadable).
- Client credentials needs a client secret. An open-source app cannot keep one, so it is only usable when each user registers their own app. It gives catalog access, not user data.

Scopes (THIRD_PARTY unless noted):

| Scope | Meaning (from spec) |
|---|---|
| `playback` | Play media content and control playback |
| `user.read` | Account info such as country and email |
| `collection.read` / `collection.write` | Read / write "My Collection" |
| `playlists.read` / `playlists.write` | List / modify user playlists |
| `recommendations.read` | Personal recommendations |
| `search.read` / `search.write` | Personalized search / delete search history |
| `entitlements.read` | What the user is entitled to (no THIRD_PARTY endpoint uses it in this spec version) |
| `r_usr` / `w_usr` | Read / write all user data. **INTERNAL tier** (see open issue above) |

## Playback

`GET /trackManifests/{id}` (THIRD_PARTY). Security: `Client_Credentials`, or PKCE with `playback` + `r_usr`.

Required query params:

| Param | Values |
|---|---|
| `formats` (array) | `HEAACV1`, `AACLC`, `FLAC`, `FLAC_HIRES`, `EAC3_JOC` |
| `manifestType` | `HLS`, `MPEG_DASH` |
| `uriScheme` | `HTTPS`, `DATA` |
| `usage` | `PLAYBACK`, `DOWNLOAD` |
| `adaptive` | boolean |

Response attributes:

- `uri`: manifest URI (or a `data:` URI with `uriScheme=DATA`).
- `formats`: the formats actually present in the manifest.
- `trackPresentation`: `FULL` or `PREVIEW`.
- `previewReason`: `FULL_REQUIRES_SUBSCRIPTION`, `FULL_REQUIRES_PURCHASE`, `FULL_REQUIRES_HIGHER_ACCESS_TIER`.
- `drmData`: `drmSystem` (`FAIRPLAY` or `WIDEVINE`), `licenseUrl`, `certificateUrl`, `initData`. "Absence implies no DRM."
- `trackAudioNormalizationData` / `albumAudioNormalizationData`: `replayGain`, `peakAmplitude`.
- `hash`: unique manifest hash.
- **No bit depth or sample rate fields.** These must be read from the DASH/HLS manifest or the decoded stream.

403 codes: `CLIENT_NOT_ENTITLED`, `CONCURRENT_PLAYBACK` ("Account is playing on another app or device"), `GEO_RESTRICTED`, `PREREQUISITE_MISSING`, `PURCHASE_REQUIRED`.

Not available to THIRD_PARTY: `/trackFiles/{id}` (PARTNER), `/playQueues` and `/userPlaybackStates` (INTERNAL). The play queue is therefore local only, with no sync to other TIDAL apps.

### Playback risks for this project

1. **DRM.** If manifests for a THIRD_PARTY app include Widevine `drmData`, mpv/ffmpeg cannot decrypt them, and bit-perfect playback outside TIDAL's Player SDK is not possible.
2. **Previews only.** `FULL_REQUIRES_HIGHER_ACCESS_TIER` means some access tiers get 30-second previews instead of full tracks. It is unknown whether THIRD_PARTY, THIRD_PARTY_PROD, or both are affected.
3. **Concurrency.** Playing elsewhere on the same account can make manifest requests fail with `CONCURRENT_PLAYBACK`. The UI needs a clear message for this.

## Catalog and search (THIRD_PARTY)

- `GET /searchResults?filter[query]=...` plus `/searchResults/{id}/relationships/{albums,artists,playlists,topHits,tracks}`. Params include `explicitFilter` (`INCLUDE`/`EXCLUDE`). Client credentials, or PKCE with `search.read` + `r_usr`.
- `GET /searchSuggestions` and `/searchSuggestions/{id}/relationships/directHits` (type-ahead).
- `GET /tracks/{id}`, `/albums/{id}`, `/artists/{id}`, `/playlists/{id}`, and batch `GET /tracks`, `/albums`, `/artists` with `filter[id]`.
- Album tracks: `/albums/{id}/relationships/items`. Artist: `/relationships/albums`, `/relationships/tracks`, `/relationships/biography`.
- Track attribute `mediaTags` carries quality tags (example `HIRES_LOSSLESS`), usable to badge hi-res tracks in lists before playback.
- `usageRules` (`/tracks/{id}/relationships/usageRules`) gives `subscription`/`free`/`paid` usage types (`STREAM`, `DJ`, `STEM`, `DOWNLOAD`) per country. Track `availability` is deprecated in favor of this.

## Library (THIRD_PARTY)

- Collections: `/userCollection{Tracks,Albums,Artists,Playlists,Videos}/me` with `/relationships/items`. GET needs `collection.read`; POST/DELETE items need `collection.write`.
- Item sort: `addedAt` (default `-addedAt`), `artists.name`, `duration`, `title`.
- **Library text search is not available**: `filter[self][matchesQuery]` on collection items returns 403 requiring INTERNAL. Filtering a library view must happen client-side.
- Playlists: `GET /playlists?filter[owners.id]=me` (`playlists.read`), `POST /playlists` (create; name 1-250 chars, `accessType` `PUBLIC`/`UNLISTED`), `PATCH`/`DELETE /playlists/{id}`, and items `POST` (add up to 50, `meta.onDuplicates` `ADD`/`FAIL`/`SKIP`, optional `positionBefore`), `DELETE` (by `meta.itemId`, up to 50), `PATCH` (reorder). All writes need `playlists.write`.
- Full playlist management is therefore possible at THIRD_PARTY.

## Discovery (THIRD_PARTY)

- Personal mixes (`recommendations.read`): `/userDailyMixes/me`, `/userDiscoveryMixes/me`, `/userNewReleaseMixes/me`, each with `/relationships/items` (items are playlists).
- Radio: `/tracks/{id}/relationships/radio`, `/artists/{id}/relationships/radio`.
- Similar: `/tracks/{id}/relationships/similarTracks`, `/artists/{id}/relationships/similarArtists`, `/albums/{id}/relationships/similarAlbums`.
- **Not available:** the home feed (`/userRecommendationBlocks` is INTERNAL), genre browsing (album genres are INTERNAL and there is no `/genres` path), and editorial pages.

## Account

`GET /users/me` (PKCE, `user.read` + `r_usr`) returns `username`, `country`, `developerAccessTier` (required), and `email`, `emailVerified`, `firstName`, `lastName`.

**No subscription tier or maximum quality field.** Max quality has to be inferred from what manifests return.

## Not verified (portal pages unreadable)

- App registration, client secret issuance, and how to move from THIRD_PARTY to THIRD_PARTY_PROD.
- Allowed redirect URIs (loopback for a terminal app).
- Token lifetimes and refresh token behavior.
- Terms for open-source clients (embedding a client ID, branding, caching).
- Numeric rate limits.

## Registered app (2026-11-21)

- A TIDAL developer app is registered. Credentials live in the git-ignored `.env` (`TIDAL_CLIENT_ID`, `TIDAL_CLIENT_SECRET`). The PKCE flow uses only the client ID.
- Allowed scopes in the dashboard: `collection.read`, `collection.write`, `entitlements.read`, `playback`, `playlists.read`, `playlists.write`, `recommendations.read`, `search.read`, `search.write`, `user.read`. **`r_usr` and `w_usr` are not offered**, which matches their INTERNAL tier in the spec. TASK-001 must test whether endpoints that list `r_usr` work without it.
- Registered redirect URIs: `http://127.0.0.1:8765/callback` and `http://localhost:8765/callback`. The dashboard accepted both, and login used the `127.0.0.1` one.
- Account: paid subscription with hi-res ("Max") access.
- Spike test tracks, both "Max" 24-bit/96 kHz: `330930030` (https://tidal.com/track/330930030/u) and `317688868` (https://tidal.com/track/317688868/u).

## TASK-001 spike results (2026-11-21)

Tool: `cmd/tidal-spike` (throwaway, standard library only). Developer access tier reported by `/users/me`: `THIRD_PARTY`.

### Login and tokens

- Authorization code + PKCE with a loopback redirect works: the browser opens `login.tidal.com/authorize`, the redirect reaches `http://127.0.0.1:8765/callback`, and the code exchange at `auth.tidal.com/v1/oauth2/token` succeeds with only `client_id` and `code_verifier` (no secret).
- Granted scopes: all 10 requested (`collection.read`, `collection.write`, `entitlements.read`, `playback`, `playlists.read`, `playlists.write`, `recommendations.read`, `search.read`, `search.write`, `user.read`). `r_usr` is not granted.
- Access token lifetime: 14400 s (4 hours). A refresh token is issued. Refresh (`grant_type=refresh_token`, `client_id`, `refresh_token`) works and does **not** rotate the refresh token.
- The token response includes a numeric `user_id`.

### `r_usr` is not needed in practice

Every probed endpoint that the spec lists with `r_usr` returned 200 with real data, using a token that does not have `r_usr`:
`/users/me`, `/searchResults` (with `include=tracks`), `/userCollectionTracks/me/relationships/items`, `/userCollectionAlbums/me/relationships/items`, `/playlists?filter[owners.id]=me`, `/userDailyMixes/me/relationships/items`, `/userDiscoveryMixes/me/relationships/items`, `/userNewReleaseMixes/me/relationships/items`, and `/trackManifests/{id}`.

`/users/me` attributes returned: `country`, `developerAccessTier`, `emailVerified`, `username` (the account email). There is no subscription field, as expected.

### Playback manifests: full tracks require a higher access tier

Test tracks `330930030` and `317688868` both have `mediaTags` `HIRES_LOSSLESS`, `LOSSLESS`.

| Request | Status | trackPresentation | previewReason | Delivered |
|---|---|---|---|---|
| DASH, `FLAC_HIRES,FLAC` (330930030) | 200 | PREVIEW | FULL_REQUIRES_HIGHER_ACCESS_TIER | FLAC 24-bit/192 kHz, 30 s |
| DASH, `FLAC_HIRES,FLAC` (317688868) | 200 | PREVIEW | FULL_REQUIRES_HIGHER_ACCESS_TIER | FLAC 24-bit/96 kHz, 30 s |
| DASH, `FLAC` | 200 | PREVIEW | same | FLAC 16-bit/44.1 kHz |
| DASH, `AACLC` | 200 | PREVIEW | same | AAC-LC 44.1 kHz |
| HLS, `FLAC_HIRES,FLAC` | 200 | PREVIEW | same | `fLaC` HLS variant |
| DASH, `uriScheme=DATA` | 200 | PREVIEW | same | MPD inline as base64 data URI |
| DASH, `adaptive=true` | 200 | PREVIEW | same | FLAC 16/44.1 and FLAC_HIRES 24/192 representations |
| DASH, `EAC3_JOC` | 403 | | | |

- **No DRM**: `drmData` is absent on every manifest, and the MPDs have no `ContentProtection` elements.
- The MPD uses `SegmentTemplate` with absolute, signed `https://sp-ad-cf.audio.tidal.com/mediatracks/...` URLs for the init segment and numbered media segments (fragmented MP4 with FLAC inside).
- Manifest URIs point to `https://im-cf.manifest.tidal.com/1/manifests/<id>.mpd` with a signed query string.
- Manifests include ReplayGain data (for example -2.07 dB, peak 0.8939 for 330930030).
- Track 330930030 is delivered as 24-bit/**192 kHz**, although the TIDAL app labels it "Max 24-bit 96 kHz".

### mpv playback

mpv (via FFmpeg's DASH demuxer) plays the preview manifest. Reported: `flac 2ch 192000 Hz`; the audio output was opened at `192000Hz stereo 2ch s32` (tested with `--ao=null`, so no hardware test yet).
When the MPD is passed as a local file, FFmpeg only allows file protocols, so mpv needs `--demuxer-lavf-o=protocol_whitelist=[file,http,https,tls,tcp,crypto,data]` to fetch the https segments. Passing the manifest via a local file keeps signed URLs out of the process list.

### Blocker: THIRD_PARTY receives 30-second previews only

The official API works end to end for a legitimate client, with no DRM, but the THIRD_PARTY developer tier only gets previews (`FULL_REQUIRES_HIGHER_ACCESS_TIER`). The `developerAccessTier` enum includes `THIRD_PARTY_PROD`, which is probably the production tier that unlocks full tracks. This is unconfirmed.

Next step, per decision-001: ask TIDAL how a third-party app gets the tier that serves full tracks (likely `THIRD_PARTY_PROD`), and what the requirements are (review, branding, open-source distribution of the client ID). Check the developer dashboard for a production access request first, then contact TIDAL developer support. Until then, development can continue against 30-second previews, which exercise the whole pipeline (manifest, DASH, FLAC hi-res, mpv, bit-perfect output).
