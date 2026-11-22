---
id: doc-004
title: 'Evaluation: ALSA hardware volume on USB DACs'
type: other
created_date: '2026-11-22T20:27:29Z'
updated_date: '2026-11-22T20:28:05Z'
---
# Evaluation: ALSA hardware volume on USB DACs

Written for TASK-018 AC #4 on 2026-11-22 (PLAN.md decision 88). This is a desk evaluation: the Schiit Vestri used so far has only a `PCM` mute switch and no volume control (doc-003), so nothing here has been measured on a DAC with a mixer. The "To test" section lists what to check when one is available.

## How USB DAC volume works

- A USB Audio Class (UAC1 or UAC2) DAC can describe a Feature Unit with a Volume control (and often Mute) in its descriptors. The kernel's `snd-usb-audio` driver exposes it as an ALSA mixer element, usually named `PCM` or after the output terminal (for example `Speaker` or `Headphone`), with a dB range read from the device.
- Setting the element sends a class-specific control request (`SET_CUR`) to the DAC over the control endpoint. The audio data goes over a separate isochronous endpoint and is not touched by it: the PCM samples needle writes to `hw:` stay exactly as they were.
- So **the USB stream stays bit-perfect.** Whatever attenuation happens is inside the DAC, usually in the DAC chip's own digital volume (for example on ESS Sabre parts) or in an analog stage. Either way it is part of the DAC, like its analog knob, and needle's indicator is right to stay "bit-perfect": it describes what reaches the device.
- The mixer is on the ALSA control device (`/dev/snd/controlC<n>`), not the PCM device, so it can be changed while needle holds `hw:` in exclusive mode. `alsamixer -c <n>` and `amixer -c <n>` work at the same time as playback.

## How needle could drive it

Not built (decision 88, option a). If it is built later, as `audio.volume = "hardware"`:

- **Find the element.** The card comes from `audio.device` (`ParseALSAHW` already gives it). `/proc/asound/card<n>/usbmixer` lists the USB mixer units for USB cards. `amixer -c <n> scontrols` lists the simple controls; pick the playback volume element (`PCM` first, then a single other playback volume). If there is none, or more than one candidate, fall back to `device` and say why.
- **Set it.** Two routes:
  - Shell out to `amixer -c <n> sset PCM 70%` (or a dB value). Simple, but it adds a runtime dependency on alsa-utils, and the output has to be parsed to read the current level.
  - Talk to the control device directly with the ALSA ctl ioctls (`SNDRV_CTL_IOCTL_ELEM_LIST`, `ELEM_INFO`, `ELEM_READ`, `ELEM_WRITE`, `TLV_READ` for the dB scale) from Go through `golang.org/x/sys/unix` (already in `go.mod` as an indirect dependency; using it directly would make it a direct one). No cgo or alsa-utils, but more code to test, and it needs fakes in place of `/dev/snd`.
- **Scale.** Map the volume keys to dB steps (for example 2 dB per press) rather than raw values, since raw steps are device-specific and often coarse at one end.
- **Startup and quit.** The DAC keeps its last level across needle runs and other apps. needle should read it, not reset it.

## Risks

- **Controls that do nothing or lie.** Some DACs advertise a volume control the hardware ignores, or report a wrong dB range. `snd-usb-audio` has per-device quirks for known cases and logs warnings such as "mixer resolution" or "cannot get min/max values". needle cannot detect a control that is ignored.
- **Loud jumps.** A wrong range or a write in raw units can jump straight to full volume. A hardware mode must clamp to the reported range and start from the DAC's current level, never from 100%.
- **Shared mode already uses it.** In shared mode, WirePlumber normally drives the ALSA hardware mixer for the sink's volume when the card has one, so the system volume keys change the DAC's volume already. A needle hardware mode would matter mainly in exclusive mode, where PipeWire is not involved.
- **Other apps.** The mixer is shared with everything else using the card, so a change from needle is a system-wide change for that DAC.

## Recommendation

Keep `device` and `software` (TASK-018). Users with a DAC that has a volume control get bit-perfect volume today by using it through `alsamixer` or the DAC itself, which the README says. Build `hardware` only once a DAC with a mixer is at hand to test with, preferably through the ctl ioctls so there is no runtime dependency on alsa-utils.

## To test, with a DAC that has a volume control

1. `amixer -c <n> scontrols` and `cat /proc/asound/card<n>/usbmixer`: note the element name and range (`amixer -c <n> sget PCM`).
2. Play a 24/96 track in exclusive mode with `audio.volume = "device"`. Change the level with `amixer`. The indicator must stay "bit-perfect", mpv's `volume` must stay 100, `af` empty, and `/proc/asound/card<n>/pcm0p/sub0/hw_params` unchanged.
3. Check the level is actually applied (it gets quieter), across the whole range, and that 0% or the minimum is not full volume.
4. Optional, for proof: a DAC with a digital output (S/PDIF) into a capture device, comparing a captured null test or checksum with the source at several levels. The digital output usually comes before the DAC's volume, so this mainly proves the USB stream is unchanged.
