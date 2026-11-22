# needle

A keyboard-driven terminal client for [TIDAL](https://tidal.com) with bit-perfect playback, built in Go on the [Charm](https://charm.sh) stack.

needle is a legitimate client: it uses only the [official TIDAL API](https://developer.tidal.com) with its own registered app, and never uses private endpoints, borrowed credentials, or DRM circumvention.

needle is an independent project and is not affiliated with or endorsed by TIDAL.

## Status

Early development. Configuration, the API client, login, the play queue, stream selection, the audio engine, the terminal UI shell, Search (with playback from results), the Library (favorites and playlists), and Discovery (mixes, radio, and similar items) are in place. The now-playing bar, the Queue view (kept across restarts), the Account and Settings views, the spectrum visualizer, and desktop media controls (MPRIS) are in place too. See `PLAN.md` for the plan and decisions, and run `backlog board` for task status.

The TIDAL developer access tier this project currently has serves **30-second previews** only. Full-track playback needs a higher tier, which is being requested from TIDAL. Previews are full-quality DRM-free FLAC (up to 24-bit/192 kHz), so the whole playback pipeline can be developed and tested against them.

## Requirements

- **Linux** (v1 targets Linux only)
- **Go 1.27** or later to build
- **[mpv](https://mpv.io)** 0.38 or later at runtime, used as the audio engine
- **PipeWire** for shared mode (`pw-dump` is used to check the output format)
- A TIDAL subscription

## Build and run

```sh
go build -o needle ./cmd/needle
./needle --version
```

Release builds stamp the version and the TIDAL client ID at link time:

```sh
go build -ldflags "-X main.version=v0.1.0 -X github.com/five07/needle/internal/config.DefaultClientID=<client-id>" \
  -o needle ./cmd/needle
```

The client ID is not stored in the source. Without a build-time ID, set `TIDAL_CLIENT_ID` in the environment or `auth.client_id` in the config file.

## Login

On the first launch needle prints a TIDAL login URL and opens it in your browser (via `xdg-open`). After you approve access, TIDAL redirects to `http://127.0.0.1:8765/callback`, where needle listens on the loopback interface only, for one callback, for up to five minutes. The login uses OAuth 2 with PKCE, so no client secret is needed. Port 8765 must be free during login, because it is the redirect URI registered for the app.

Tokens are kept in the system keyring through the Secret Service API (GNOME Keyring, KWallet, KeePassXC). When no keyring is reachable, needle stores them in `$XDG_CONFIG_HOME/needle/credentials.json` with mode `0600` instead, logs a warning, and refuses to read that file if other users can access it. Only one account is stored; logging in again replaces it.

Access tokens refresh automatically. If TIDAL revokes the session, needle asks you to log in again.

To log out and remove stored credentials from both the keyring and the file (this also forgets the highest quality shown in the Account view and the saved queue):

```sh
./needle -logout
```

After login, needle prints the account name and opens the terminal UI.

## Using the terminal UI

The screen has a sidebar of sections (Search, Library, Discovery, Queue, Account, Settings), a main pane for the selected section, a now-playing bar, and a line of key hints. It needs at least 50x16 cells and is laid out for 80x24 and larger. Below 80 columns the sidebar shrinks to a strip of initials (`≡` for Settings). When the terminal is too short for all six sections, the sidebar scrolls to keep the current one in view. In a smaller terminal it shows "Terminal too small" until the window grows again.

Sections that are not built yet say so in the main pane. Playing and queueing are confirmed by a short notice in the hint line, and playback errors (for example a missing mpv) appear there too.

### Keys

| Key | Action |
|---|---|
| `j` / `k`, `↓` / `↑` | Move down / up |
| `h` / `←` | Focus the sidebar, or go back from an opened item |
| `l` / `→`, `enter` (sidebar) | Focus the main pane |
| `g` / `G` | Jump to the top / bottom |
| `ctrl+d` / `ctrl+u` | Half a page down / up |
| `enter` | Play the selected track, or open the selected album, artist, or playlist. On the track that is playing (or paused), it pauses (or resumes) instead of starting over |
| `a` | Add the selected track, album, or playlist to the end of the queue |
| `A` | Play the selected track, album, or playlist next |
| `tab` / `shift+tab` | Switch to the next / previous tab (Search results, an artist, Library, Discovery) |
| `f` / `F` | Add the selected track, album, artist, or playlist to your favorites / remove it |
| `+` | Add the selected track to one of your playlists |
| `R` | Start radio for the selected track or artist |
| `S` | List tracks, artists, or albums similar to the selected one |
| `o` | Change the sort order of the current Library tab |
| `esc`, `backspace` | Go back, leave the search input, or clear the Library filter |
| `/` | Go to Search and type a query (in Library: filter the current tab) |
| `1` to `6` | Jump to a sidebar section |
| `?` | Show or hide every key for the current screen (`j`/`k` scroll it when it does not fit) |
| `q` | Quit |
| `ctrl+c` | Quit, even while typing |
| `space` | Pause / resume, or play the current track when stopped |
| `n` / `]` | Next track |
| `p` / `[` | Previous track (on the first track: start it over) |
| `s` | Shuffle on / off |
| `r` | Repeat: off, all, one |
| `c` | Autoplay radio: on, off |
| `v` | Visualizer: on, off |
| `-` / `=` | Volume down / up by 5% (software volume; see Volume) |

While the search input has focus, letters, `-`, `=`, and `space` type into it, so `q`, `j`, `space`, and the other single-letter keys do not act. Press `esc` (or `enter`, or `tab` to go straight to the next tab) to leave the input.

The hint line shows as many keys as fit; `?` lists them all. Tabbed views put their most important keys first so they stay visible on small terminals: `/ search` and `tab` in Search, `tab` in an artist view, `/ filter` and `tab` in the Library, and `tab` in Discovery.

### Search

Type in the search input and results appear after a short pause (300 ms), or press `enter` to search at once. Results appear in tabs, **Tracks**, **Albums**, **Artists**, and **Playlists**, with the first 20 of each. A kind with no results gets no tab, and each new search opens the first tab with results. Switch with `tab` / `shift+tab` or by clicking a tab name; `tab` also works while you are typing, leaving the input and going to the next tab. Only the newest search is shown: an older request still in flight is canceled, and a late answer to it is ignored. Searching never interrupts playback, and a search keeps running if you switch to another section.

- **Enter on a track** replaces the queue with the tracks of the list it is in (the Tracks tab, or the album or playlist), starting at that track.
- **`a` and `A`** queue one track, or every track of an album or playlist, up to 500 tracks (a notice says when the rest were left out). On an empty queue the first queued track starts playing. Artists are not queued directly: open the artist and queue its tracks.
- **Opening an album or playlist** shows its tracks, loading more as you scroll toward the end. **Opening an artist** shows their **Top tracks** and **Albums** in tabs (a tab with nothing in it is left out); albums load more as you scroll.
- Search uses your account's country, so results match what you can play.

### Library

The Library has four tabs: **Playlists** (the ones you own and the ones you added, without folders), **Albums**, **Tracks**, and **Artists**. Switch with `tab` / `shift+tab` or by clicking a tab name. Each tab loads the first time you open it, newest added first, and loads more as you scroll toward the end. Play, queue, and open items with the same keys as in Search.

- **`o`** cycles the server-side sort of the tab (date added, title or name, and artist for albums and tracks) and reloads it.
- **`/`** filters the current tab by title, artist, and album, ignoring case and accents. While a filter is set, the rest of the tab loads in the background so the filter covers all of it, up to 5000 items (a notice says when it stops there). `enter` keeps the filter and returns to the list; `esc` clears it.
- **`f` / `F`** work in every view. TIDAL has no way to ask whether one item is a favorite, so a `♥` marks only items needle knows about: ones loaded in the Library or favorited this session. Removing a favorite in the Library drops its row; adding one elsewhere reloads the matching tab.
- **`+`** on a track opens a list of the playlists you own. `enter` adds the track to the selected one and `esc` cancels. A track already in that playlist is not added twice.

### Discovery

Discovery shows the mixes TIDAL makes for you in tabs: **Daily** (your daily mixes), **Discovery** (new and familiar artists), and **New Releases**. A kind with no mixes gets no tab. Mixes load the first time you open Discovery and again when you come back more than an hour later, since TIDAL updates them every morning. Mix names use your system language (`LC_ALL`, `LC_MESSAGES`, or `LANG`) when TIDAL supports it.

Open a mix to see its tracks; play and queue it like a playlist. `f` and `F` add a mix to your favorites (it then appears in the Library's Playlists tab) or remove it.

- **`R`** on a track or artist, in any view, opens its radio: a mix of tracks like it, played and queued like any track list. Albums, playlists, and mixes have no radio.
- **`S`** on a track, artist, or album, in any view, lists similar tracks, artists, or albums, loading more as you scroll. Playlists and mixes have no similar list.

### Now playing

The bar at the bottom shows, centered, top to bottom:

1. The track title, bold. A title too wide for the bar scrolls across it endlessly. (Drawing it at 2x size in terminals that support kitty's text sizing is planned as its own task.)
2. The artists and the album.
3. The format: what TIDAL sent, what reaches the sound card, the audio mode, and the bit-perfect indicator, for example `FLAC 24/96 → out 24/96 · exclusive · ✓ bit-perfect`. When the output differs from the source, the indicator turns yellow and says why, for example `⚠ resampled to 48 kHz (source 96 kHz)`. Previews are marked `PREVIEW`. While the software volume is below 100%, `vol 70%` (for example) follows the indicator.
4. Elapsed time, a progress bar, and the track length. The progress bar is display only.
5. The controls: shuffle (`⇄ on`/`off`), previous, play/pause, next, repeat (`↻ off`/`all`/`one`), and autoplay (`∞ on`/`off`). Click any of them.

With the visualizer on (`v`), a spectrum of the music fills the bar's background: 32 frequency bands from 40 Hz to 10 kHz, lows on the left, drawn in the free cells around the text so no text is covered. Each bar is shaded in 4 steps, darker at the bottom and brighter at the top, in the theme chosen in Settings.

The visualizer never touches the audio you hear, so the bit-perfect indicator does not change when it is on. needle gets the music for it from a second, silent mpv that decodes the same track at a low sample rate and sends the samples to needle instead of a sound card. This means each track is downloaded a second time while the visualizer is on (about as much data again as the track itself), and it uses a little CPU. With the visualizer off, nothing extra runs. If the second decode cannot start, the bars stay empty, the reason is logged, and playback carries on.

When the queue runs out, the bar keeps the last track and says "Stopped", unless autoplay continues it.

### Queue

The Queue section lists the track that is playing, then the tracks still to come, in play order (with shuffle on, the shuffled order). The sidebar shows how many are still to come, for example `Queue (12)`.

- **`enter`** plays the selected track, or pauses and resumes the one that is playing.
- **`d`** or **`x`** removes the selected upcoming track. The playing track stays; press `n` to skip it.
- **`D`** clears every upcoming track. It asks first: press `D` again within 2 seconds. The playing track keeps playing.

The queue is kept across restarts in `$XDG_STATE_HOME/needle/queue.json` (mode `0600`): the tracks already played, the current track, the ones still to come (in their shuffled order, if shuffle is on), and the shuffle and repeat settings. It is saved after every change to it, not while a track plays, so it survives a crash too; the position within a track is not kept. When there is a saved queue, needle opens on the Queue view with the current track selected and waits: press `space`, `enter`, or the play button to start it. A damaged saved queue, or one over 10,000 tracks, is ignored (and logged), and a queue longer than that is not saved.

**Autoplay** continues with related music when the queue runs out. Turn it on with `c`, a click on `∞` in the bar, or the Autoplay row in Settings, which also saves it (`c` changes it for the session only). With autoplay on and repeat off, when the last track starts needle fetches TIDAL's track radio for it and adds its first page (about 20 tracks) to the end of the queue, leaving out tracks already queued, so the next track plays gaplessly. When those run out, it does the same from the new last track, so the music drifts. Pressing next on the last track continues too.

- Autoplayed tracks start with `∞` in the Queue view, and the hint line shows `∞ autoplay`.
- Tracks you add with `a` go ahead of the autoplayed ones; `A` (play next) still puts them right after the current track.
- Turning autoplay off removes the upcoming autoplayed tracks and keeps yours. The current track finishes.
- If TIDAL has no radio for the track, or the request fails, playback stops at the end with a notice, and needle does not try again for that track until you turn autoplay off and on.
- Autoplayed tracks are kept with the saved queue. Desktop widgets (MPRIS) can skip to the next track when autoplay is on, but cannot turn it on or off.

### Account

The Account section shows your username, country, and email from TIDAL, and the highest quality TIDAL has sent you, for example `FLAC 24/192 (first seen 2026-11-22)`. TIDAL's API does not say which plan you have, so this is the best evidence of it: it is ranked by sample rate, then bit depth, then lossless over lossy, ignores previews, and is remembered across runs in `$XDG_STATE_HOME/needle/best_quality.json`. Settings are in their own section.

### Settings

The Settings section holds every setting that is not about your TIDAL account. Select a row and press `enter` (or double-click it) to change it:

- **Visualizer**: on or off.
- **Visualizer theme**: cycles through `terminal` (the terminal's own blues, so it follows your terminal theme), `burnt orange`, `ice blue`, `forest green`, `royal purple`, `deep red`, and `custom` (offered only when `visualizer.custom` is set in `config.toml`; the colors themselves are edited there).
- **Quality**: cycles through `aac`, `lossless`, and `hires`. The track playing keeps its quality; the new one applies from the next track, and a notice says so.
- **Audio mode**: switches between `shared` and `exclusive`. Exclusive mode needs a sound card, so switching to it opens the device list first, and nothing changes unless you pick one (`esc` cancels). Switching back to shared sets the device to `auto`.
- **Device**: opens the list of devices for the current mode, the same ones `needle -list-devices` shows, with the current one marked. In shared mode `auto` (the system default) comes first.
- **Exclusive volume**: `on the output device` or `software` (see Volume). It applies at once, without restarting the audio engine.
- **Autoplay**: on or off.

Changes apply at once, except the audio mode and device, which apply after a second without further changes, so cycling through choices does not restart playback each time. Then needle stops the audio engine and starts it again with the new output, resuming the current track where it was (paused stays paused). Expect a short gap; if the new device is busy or missing, the bar shows the usual playback error.

Every change is also saved to `config.toml` once the changes settle (and at quit). Only the changed lines are rewritten, so your comments and layout stay as they are; a key that is missing is added under its table. If the file writes a setting as a dotted key (`audio.mode = "shared"` at the top level) or an inline table (`audio = { mode = "shared" }`), needle cannot update it in place: the change applies for the session and a notice asks you to edit the file by hand. If saving fails for another reason, a notice says so, the reason is logged, and the change still applies for the session.

`v` and `c` stay quick toggles for the session and are not saved. When the session differs from the saved setting, the row shows both, for example `off (now on)`.

### Mouse

Click a sidebar section to switch to it, or a row to select it. Double-click opens or plays. The scroll wheel scrolls the list under the pointer. Click the controls in the now-playing bar to use them. Clicking anywhere closes the help.

## Playback

needle asks TIDAL for a DASH stream in the formats allowed by the `quality` setting, best first, so TIDAL falls back automatically when a format is unavailable:

| `quality` | Formats requested |
|---|---|
| `hires` (default) | FLAC hi-res, FLAC, AAC |
| `lossless` | FLAC, AAC |
| `aac` | AAC |

The format TIDAL actually delivers is reported to the player. Dolby Atmos is never requested, because it cannot be played as bit-perfect stereo.

- **Previews** are labeled as previews, with TIDAL's reason (for example, the app's developer access only allows previews). They are never presented as full tracks.
- **DRM-protected streams** are refused with a clear message. needle does not decrypt DRM.
- **Playing on another device** with the same account blocks playback here. needle tells you to stop it there first.
- TIDAL's signed stream URLs expire after an hour. needle fetches a fresh stream when the current one would expire before the track ends, and prefetches the next track in the queue for gapless playback.

## Audio output

needle plays through mpv, run as a private subprocess controlled over a JSON IPC socket in `$XDG_RUNTIME_DIR/needle/` (mode `0700`). Stream manifests are written there with mode `0600` and deleted after use, so signed URLs never appear on a command line. mpv runs with `--no-config`, so your own mpv settings cannot change the output.

There are two output modes, set with `audio.mode`:

- **`shared`** (default) plays through PipeWire, alongside other apps. PipeWire switches the device to the track's sample rate when nothing else is playing; if another app holds the device at a different rate, PipeWire resamples.
- **`exclusive`** opens an ALSA hardware device directly, so samples reach the DAC unchanged. Other apps cannot use the device while needle plays, and if another app is already using it, playback stops with an error instead of quietly falling back to shared mode. Exclusive mode needs an explicit device (`auto` is rejected at startup).

To see the device names for `audio.device`:

```sh
./needle -list-devices
```

Exclusive mode needs an `alsa/hw:CARD=<name>,DEV=<n>` device from the first list. Devices that are not USB are flagged as built-in: on some laptops (for example Apple Silicon Macs running Asahi Linux) the built-in device drives the speakers without any protection, so do not use it in exclusive mode. Shared mode takes `auto` or a `pipewire/<sink>` name from the second list.

### Bit-perfect indicator

needle compares the source format with what the sound card is actually running, read from `/proc/asound`, not just what mpv reports. The indicator has three states:

| State | Meaning |
|---|---|
| bit-perfect | Exclusive mode, lossless source, and the card runs at the source sample rate with enough bit depth (24-bit audio carried in a 32-bit format counts). |
| rate matched | Shared mode at the source rate, or a lossy (AAC) source. PipeWire may still mix or convert, so this is not claimed as bit-perfect. |
| altered | Resampled, software volume below 100%, audio filters active, bit depth reduced, or the output format cannot be verified (for example a PipeWire filter sink with no ALSA card behind it). |

The source bit depth comes from TIDAL's stream description. When it is missing, the indicator requires 24-bit output before claiming bit-perfect.

### Volume

`-` and `=` lower and raise needle's software volume in 5% steps, with a notice of the new level. Software volume never goes above 100% (no gain), and every start is at 100%, so playback starts bit-perfect. The level is not saved, but it lasts across audio output switches while software volume is allowed. Desktop widgets can change it too (see MPRIS).

How volume works in exclusive mode is the `audio.volume` setting, also the Exclusive volume row in Settings:

- `device` (the default): software volume is disabled in exclusive mode. Set the volume on your DAC or amplifier; the volume keys say so. Any software volume change would alter the samples.
- `software`: the volume keys work in exclusive mode too. Below 100% the samples change, so the indicator shows "altered" (`software volume 70%`); back at 100% it shows bit-perfect again. Switching back to `device` returns the volume to 100%.

Shared mode always has software volume, and the indicator shows "altered" whenever it is below 100%.

Some USB DACs have their own volume control, which ALSA lists as a mixer control (for example in `alsamixer`). It is applied inside the DAC, so the samples sent over USB stay unchanged and needle's indicator stays bit-perfect. needle does not drive it; use `alsamixer` or the DAC's own control. Backlog doc-004 has the evaluation.

### Gapless playback

The next track in the queue is fetched about 45 seconds before the current one ends and handed to mpv in advance. Consecutive tracks with the same format play without a gap. When the format changes (for example 44.1 kHz to 96 kHz), the device is reopened at the new rate, which leaves a short gap; needle never resamples to avoid it. Repeat-one replays the track without preloading.

### Errors

A missing mpv, a busy device, an output that cannot be opened, or an mpv crash stops playback with a message instead of hanging. A track that is no longer on TIDAL, or not available in your country, is skipped with a notice and the next one plays, in any queue; if every track in the queue fails that way, playback stops with a message instead of going round with repeat on. Other per-track errors (a track that must be purchased, or that TIDAL does not let this app play) still stop playback with a message. If a stream breaks mid-track (usually because its signed links expired), needle fetches a fresh stream once and resumes from the same position.

### Quitting

Quitting while a track plays fades the output to silence first (about 100 ms), then waits about 200 ms for audio already in the device buffer to play out, so the stream is not cut mid-waveform with an audible pop. This makes quitting about 300 ms slower, only while something is playing. The fade changes the volume only after you quit, so it does not affect bit-perfect playback. If mpv stops responding, quitting still ends within 2 seconds.

## Desktop integration (MPRIS)

While it runs, needle appears on the D-Bus session bus as `org.mpris.MediaPlayer2.needle` (a second copy adds `.instance<pid>`), so media keys, `playerctl`, and bar widgets such as Waybar's `mpris` module and omarchy-shell can show and control it:

- **Shown:** title, artists, album, length, the TIDAL track link, playing/paused/stopped, shuffle, and repeat (`None`, `Track`, or `Playlist`), updated as they change.
- **Controls:** play, pause, play/pause, stop, next, previous, setting shuffle and repeat, and setting the volume. Volume writes set the software volume (values below 0 count as 0, above 1 as 1) wherever the volume keys work; while the volume is on the output device they are refused with `NotSupported`.
- **Not supported:** seeking (`CanSeek` is false), album art, and raising or quitting the app.

Without a session bus (for example over SSH), needle logs a warning and runs without desktop integration. Try it with:

```sh
playerctl -p needle metadata
playerctl -p needle play-pause
```

## Configuration

needle reads `$XDG_CONFIG_HOME/needle/config.toml` (default `~/.config/needle/config.toml`). The file is optional and every key has a default:

```toml
# Stream quality: "aac", "lossless", or "hires".
quality = "hires"
# Continue with track radio when the queue runs out (toggle with c).
autoplay = false

[audio]
# "shared" plays through PipeWire. "exclusive" opens the device directly
# for bit-perfect output, blocking other apps while playing.
mode = "shared"
# mpv audio device name (see needle -list-devices). Shared mode: "auto"
# or "pipewire/<sink>". Exclusive mode: "alsa/hw:CARD=<name>,DEV=<n>".
device = "auto"
# Volume in exclusive mode: "device" (set it on your DAC or amp) or
# "software" (the volume keys work; below 100% it is not bit-perfect).
volume = "device"

[visualizer]
# Start with the spectrum visualizer on (toggle with v). It downloads each
# track a second time while on; see "Now playing".
enabled = false
# "terminal", "burnt orange", "ice blue", "forest green", "royal purple",
# "deep red", or "custom".
theme = "terminal"
# For theme = "custom": 4 colors "#rrggbb", bottom (darkest) to top.
# custom = ["#202020", "#505050", "#a0a0a0", "#ffffff"]

[auth]
# TIDAL developer app client ID. Overrides TIDAL_CLIENT_ID and the
# build-time default. Letters, digits, ".", "_" and "-" only.
# client_id = ""
```

Unknown keys and invalid values stop startup with an error that names the key. The Settings section saves its changes here too (see Settings), keeping your comments; it never writes `auth` or `visualizer.custom`.

The client ID is chosen in this order: `auth.client_id` in the config file, then the `TIDAL_CLIENT_ID` environment variable, then the build-time default.

Logs are written to `$XDG_STATE_HOME/needle/needle.log` (default `~/.local/state/needle/needle.log`). The directory is created with mode `0700` and the file with mode `0600`. Tokens are never logged.

## Development

| Task | Command |
|---|---|
| Run tests (with race detector) | `go test -race ./...` |
| Coverage for a package | `go test -cover ./cmd/needle` |
| Vet | `go vet ./...` |
| Check formatting (should print nothing) | `gofmt -l .` |
| Fix formatting | `gofmt -w .` |

CI runs the formatting check, vet, race-enabled tests, and a build on pull requests into `main` and on `main` after merge (`.github/workflows/ci.yml`). Pushes to other branches do not run CI, so run the checks above locally.

Tasks, docs, and decisions are tracked with [Backlog.md](https://github.com/MrLesk/Backlog.md) under `.backlog/`.

### TIDAL credentials

Developers need their own TIDAL developer app (https://developer.tidal.com) with the redirect URI `http://127.0.0.1:8765/callback`. Copy `.env.example` to `.env` and fill in the client ID. `.env` is git-ignored; never commit it.

The app does not read `.env` itself. Export it into your shell for development runs:

```sh
set -a; . ./.env; set +a
go run ./cmd/needle
```

### API spike tool

`cmd/tidal-spike` is a throwaway probe of the official API, kept as a debugging aid until the real API client replaces it. It logs in with PKCE, then inspects endpoints and track manifests:

```sh
export TIDAL_SPIKE_TOKENS="$HOME/.cache/tidal-spike/tokens.json"
go run ./cmd/tidal-spike login              # opens the browser
go run ./cmd/tidal-spike whoami
go run ./cmd/tidal-spike manifest 330930030 # manifest summary
go run ./cmd/tidal-spike play 330930030     # plays 20 s through mpv
```

Tokens are written to `TIDAL_SPIKE_TOKENS` with `0600` permissions and are never printed.

## License

[MIT](LICENSE)
