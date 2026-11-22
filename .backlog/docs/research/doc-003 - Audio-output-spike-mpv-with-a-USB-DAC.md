---
id: doc-003
title: 'Audio output spike: mpv with a USB DAC'
type: other
created_date: '2026-11-22T06:05:28Z'
updated_date: '2026-11-22T06:05:28Z'
---
# Audio output spike: mpv with a USB DAC

Run on 2026-11-22 before planning TASK-008. Hardware: Apple Silicon MacBook Pro (Asahi Linux, PipeWire 1.6.9, mpv 0.41.0) with a Schiit Vestri USB DAC (ALSA card 4, `hw:CARD=Vestri,DEV=0`). Sources: 30-second TIDAL previews of track 330930030 (FLAC 24/192) and 317688868 (FLAC 24/96), fetched through `tidal.Client.TrackManifest` (this also verified the TASK-007 success path live: rates, duration, ReplayGain, and a 60-minute expiry all decoded correctly).

Method: mpv JSON IPC properties sampled during playback, compared with the kernel's view in `/proc/asound/card4/pcm0p/sub0/hw_params`.

## Device

- The Vestri accepts `S16_LE`, `S24_3LE`, and `S32_LE`, each at 44100, 48000, 88200, 96000, 176400, and 192000 Hz (`/proc/asound/card4/stream0`).
- Its ALSA mixer has only a `PCM` mute switch, no volume control. Attenuation happens on the analog knob, so the DAC never alters the digital stream.
- The built-in MacBook outputs are unsuitable for exclusive mode: the ALSA "Primary" device on Asahi is the raw speaker path that the speaker-safety convolver normally protects, and the headphone jack goes through a DSP chain. Exclusive mode must never default to them.

## Results

| Case | mpv `audio-params` / `audio-out-params` | Hardware (`hw_params`) | Bit-perfect |
|---|---|---|---|
| Exclusive `hw:`, 24/192 | s32 192000 / s32 192000, `af` empty, `volume` 100 | `S32_LE` 192000 | Yes |
| Exclusive `hw:`, 24/96 | s32 96000 / s32 96000 | `S32_LE` 96000 | Yes |
| Shared PipeWire sink, 24/192, device idle | s32 192000 / s32 192000 | `S32_LE` 192000 (PipeWire switched rate; `clock.allowed-rates` includes 192000) | Rate matches; not guaranteed (float mixing path) |
| Shared, another app holding the device at 48 kHz | s32 192000 / s32 192000 | `S32_LE` **48000** | **No: resampled, and mpv cannot see it** |
| Exclusive while PipeWire is actively using the device | n/a | n/a | mpv fails: `Playback open error: Device or resource busy`, then "audio output initialization failed", exit code 2 |

- Decoded 24-bit FLAC is carried in `s32` (MSB-aligned), which is lossless. **mpv does not expose the source bit depth**; the 24 comes only from the MPD Representation id (`FLAC_HIRES,192000,24`, undocumented) or from the FLAC stream header.
- In exclusive mode `ao-volume` reported 70% although the Vestri has no volume control, so it is not a reliable signal. `volume` (software) and `af` (filter chain) are.
- When PipeWire has the device idle-suspended, exclusive mode opens it immediately. While another app plays, exclusive mode fails with "busy" until PipeWire releases it.
- FFmpeg logs `dash: Failed to open an initialization section` at startup and then plays normally. It is noise, not a failure.

## Gapless and track-end events

- `--gapless-audio=yes` keeps the device open at the **first** track's format and **resamples** later tracks that differ (a 24/96 track after a 24/192 one played at 192 kHz). Never use it.
- `--gapless-audio=weak` (mpv's default) with `--prefetch-playlist=yes` keeps the device open across same-format tracks and reopens it at the native rate when the format changes (192000 then 96000 confirmed in `hw_params`).
- Rendering two same-format tracks with `--ao=pcm` gave a file exactly as long as the two parts (5.994 s + 5.995 s), with zero frames of digital silence anywhere: the transition is sample-accurate.
- IPC events per track: `start-file`, `file-loaded`, `playback-restart`, then `end-file` with `reason: "eof"`; `playlist-pos` changes on each start; `audio-out-params` changes only when the format changes.

## Constraints for the engine

- Unix socket paths are limited to 108 bytes. The IPC socket must live under a short private directory such as `$XDG_RUNTIME_DIR/tidal-tui/` (mode 0700), not under a long temp path.
- zsh globs `[...]` in arguments; pass `--demuxer-lavf-o=protocol_whitelist=[...]` through `exec.Command` arguments, never a shell.
- The actual output rate must be read from the card (`/proc/asound/cardN/pcmXp/subY/hw_params`), not from mpv, because PipeWire resampling is invisible to mpv. In shared mode the card has to be found from the PipeWire sink (node properties).
