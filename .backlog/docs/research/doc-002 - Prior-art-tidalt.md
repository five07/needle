---
id: doc-002
title: 'Prior art: tidalt'
type: other
created_date: '2026-11-22T04:16:46Z'
updated_date: '2026-11-22T04:19:23Z'
tags:
  - research
  - prior-art
  - tidal
---
# Prior art: tidalt

Repo: https://github.com/Benehiko/tidalt (reviewed at commit fd1978e, 2026-09-15). Go, Bubble Tea v1, Apache-2.0, Linux only. About 13.5k lines outside `vendor/`. The README says it is "100% vibe coded".

## Why it has no DRM handling

It does not use the official API. It uses the private v1 API with credentials taken from TIDAL's own app:

- `internal/tidal/client.go:17-24`: hardcoded `ClientID` and `ClientSecret` "baked into the official Tidal app", with `AuthURL https://auth.tidal.com/v1/oauth2` and `BaseURL https://api.tidal.com/v1`.
- Login uses the OAuth2 device flow, which the private API supports and the official API does not.
- Streams come from `GET /v1/tracks/{id}/urlpostpaywall?urlusagemode=STREAM&audioquality=<tier>&assetpresentation=FULL` (`internal/tidal/api.go:320`). This returns a direct file URL (`urls[0]`), with no DASH manifest and no DRM. The client tries the tiers in order `HI_RES_LOSSLESS`, `LOSSLESS`, `HIGH`, `LOW` and keeps the first one that succeeds.
- Mixes also use v1 (`/v1/pages/my_collection_my_mixes`, `/v1/mixes/{id}/items`). Their CLAUDE.md says `openapi.tidal.com/v2/userRecommendations` "was removed by Tidal and now 404s". The current spec replaces it with `userDailyMixes`, `userDiscoveryMixes`, and `userNewReleaseMixes` (see doc-001).

So tidalt shows that the unofficial path (PLAN.md Q1 option b) works today. It says nothing about whether the official `trackManifests` endpoint returns DRM for third-party apps.

## How its "bit-perfect" output works

The audio is decoded in-process with FFmpeg through cgo and written straight to an ALSA `hw:` device. There is no mpv, despite the file name `internal/player/mpv.go`.

- `internal/player/avcodec.c`: libavformat/libavcodec decode, then libswresample converts the **sample format only** to S32 at the source rate. A 16- or 24-bit integer source MSB-aligned into S32 is lossless.
- `internal/player/alsa.c`: opens `hw:` with RW interleaved access and negotiates the format and channel count.
- Before opening the device it asks PipeWire to release it through the D-Bus `org.freedesktop.ReserveDevice1.Audio{N}` reservation, and releases the device again on pause.
- If `hw:` refuses the format, it retries once with `plughw:` and reports `bitPerfect=false` to the UI.

## Problems noticed (code reading only, not run)

1. **Sample rate is not checked.** `snd_pcm_hw_params_set_rate_near` (`alsa.c`) can settle on a different rate on a `hw:` device, for example 96 kHz when the source is 192 kHz. `openALSA` (`mpv.go:435-465`) stores the negotiated rate but never compares it with the source rate. If the DAC lacks the source rate, the track plays at the wrong speed while still labeled bit-perfect.
2. **Software volume is not reflected.** `mpv.go:868-871` multiplies every sample by the volume whenever volume is not 100%. That changes the samples, but `bitPerfect` only turns false on the `plughw:` fallback.
3. **The format preference list for 24-bit sources has no effect.** The decoder always outputs S32 (`avcodec.go:23`), so ALSA is only ever offered `S32_LE`. A DAC that accepts only `S24_3LE` drops to `plughw:` even though a lossless repack was possible.
4. **The quality label can mislead.** The badge shows whichever tier succeeded first. The real bit depth and sample rate come from the decoder, but the tier string is what's labeled.

## Takeaways for tidal-tui

- tidalt's API approach (private v1 API, borrowed app credentials) is rejected for tidal-tui. See decision-001. Only its audio-path lessons apply.
- "Bit-perfect" needs exact checks: the negotiated rate must equal the source rate, and the format must be a lossless container. Software volume must either be disabled in bit-perfect mode or turn the indicator off.
- The D-Bus device-reservation handshake with PipeWire is a good pattern for an exclusive mode that cooperates with the desktop. mpv's `--audio-exclusive` with an ALSA `hw:` device should achieve the same thing, and TASK-001/TASK-008 should verify it.
