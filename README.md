# tidal-tui

A keyboard-driven terminal client for [TIDAL](https://tidal.com) with bit-perfect playback, built in Go on the [Charm](https://charm.sh) stack.

tidal-tui is a legitimate client: it uses only the [official TIDAL API](https://developer.tidal.com) with its own registered app, and never uses private endpoints, borrowed credentials, or DRM circumvention.

## Status

Early development. Configuration, the API client, login, the play queue, stream selection, the audio engine, and the terminal UI shell are in place. The Search, Library, Discovery, Queue, and Account views and the now-playing bar are being built next. See `PLAN.md` for the plan and decisions, and run `backlog board` for task status.

The TIDAL developer access tier this project currently has serves **30-second previews** only. Full-track playback needs a higher tier, which is being requested from TIDAL. Previews are full-quality DRM-free FLAC (up to 24-bit/192 kHz), so the whole playback pipeline can be developed and tested against them.

## Requirements

- **Linux** (v1 targets Linux only)
- **Go 1.27** or later to build
- **[mpv](https://mpv.io)** 0.38 or later at runtime, used as the audio engine
- **PipeWire** for shared mode (`pw-dump` is used to check the output format)
- A TIDAL subscription

## Build and run

```sh
go build -o tidal-tui ./cmd/tidal-tui
./tidal-tui --version
```

Release builds stamp the version and the TIDAL client ID at link time:

```sh
go build -ldflags "-X main.version=v0.1.0 -X github.com/five07/tidal-tui/internal/config.DefaultClientID=<client-id>" \
  -o tidal-tui ./cmd/tidal-tui
```

The client ID is not stored in the source. Without a build-time ID, set `TIDAL_CLIENT_ID` in the environment or `auth.client_id` in the config file.

## Login

On the first launch tidal-tui prints a TIDAL login URL and opens it in your browser (via `xdg-open`). After you approve access, TIDAL redirects to `http://127.0.0.1:8765/callback`, where tidal-tui listens on the loopback interface only, for one callback, for up to five minutes. The login uses OAuth 2 with PKCE, so no client secret is needed. Port 8765 must be free during login, because it is the redirect URI registered for the app.

Tokens are kept in the system keyring through the Secret Service API (GNOME Keyring, KWallet, KeePassXC). When no keyring is reachable, tidal-tui stores them in `$XDG_CONFIG_HOME/tidal-tui/credentials.json` with mode `0600` instead, logs a warning, and refuses to read that file if other users can access it. Only one account is stored; logging in again replaces it.

Access tokens refresh automatically. If TIDAL revokes the session, tidal-tui asks you to log in again.

To log out and remove stored credentials from both the keyring and the file:

```sh
./tidal-tui -logout
```

After login, tidal-tui prints the account name and opens the terminal UI.

## Using the terminal UI

The screen has a sidebar of sections (Search, Library, Discovery, Queue, Account), a main pane for the selected section, a now-playing bar, and a line of key hints. It needs at least 50x16 cells and is laid out for 80x24 and larger. Below 80 columns the sidebar shrinks to a strip of initials. In a smaller terminal it shows "Terminal too small" until the window grows again.

Sections that are not built yet say so in the main pane. Until playback is connected to the UI, the bar shows "Nothing playing".

### Keys

| Key | Action |
|---|---|
| `j` / `k`, `↓` / `↑` | Move down / up |
| `h` / `←` | Focus the sidebar, or go back from an opened item |
| `l` / `→`, `enter` (sidebar) | Focus the main pane |
| `g` / `G` | Jump to the top / bottom |
| `ctrl+d` / `ctrl+u` | Half a page down / up |
| `enter` | Open or play the selected item |
| `esc`, `backspace` | Go back, or leave the search input |
| `/` | Go to Search and type a query |
| `1` to `5` | Jump to a sidebar section |
| `?` | Show or hide every key for the current screen (`j`/`k` scroll it when it does not fit) |
| `q` | Quit |
| `ctrl+c` | Quit, even while typing |

While the search input has focus, letters type into it, so `q`, `j`, and the other single-letter keys do not act. Press `esc` (or `enter`) to leave the input.

### Mouse

Click a sidebar section to switch to it, or a row to select it. Double-click opens or plays. The scroll wheel scrolls the list under the pointer. Clicking anywhere closes the help.

## Playback

tidal-tui asks TIDAL for a DASH stream in the formats allowed by the `quality` setting, best first, so TIDAL falls back automatically when a format is unavailable:

| `quality` | Formats requested |
|---|---|
| `hires` (default) | FLAC hi-res, FLAC, AAC |
| `lossless` | FLAC, AAC |
| `aac` | AAC |

The format TIDAL actually delivers is reported to the player. Dolby Atmos is never requested, because it cannot be played as bit-perfect stereo.

- **Previews** are labeled as previews, with TIDAL's reason (for example, the app's developer access only allows previews). They are never presented as full tracks.
- **DRM-protected streams** are refused with a clear message. tidal-tui does not decrypt DRM.
- **Playing on another device** with the same account blocks playback here. tidal-tui tells you to stop it there first.
- TIDAL's signed stream URLs expire after an hour. tidal-tui fetches a fresh stream when the current one would expire before the track ends, and prefetches the next track in the queue for gapless playback.

## Audio output

tidal-tui plays through mpv, run as a private subprocess controlled over a JSON IPC socket in `$XDG_RUNTIME_DIR/tidal-tui/` (mode `0700`). Stream manifests are written there with mode `0600` and deleted after use, so signed URLs never appear on a command line. mpv runs with `--no-config`, so your own mpv settings cannot change the output.

There are two output modes, set with `audio.mode`:

- **`shared`** (default) plays through PipeWire, alongside other apps. PipeWire switches the device to the track's sample rate when nothing else is playing; if another app holds the device at a different rate, PipeWire resamples.
- **`exclusive`** opens an ALSA hardware device directly, so samples reach the DAC unchanged. Other apps cannot use the device while tidal-tui plays, and if another app is already using it, playback stops with an error instead of quietly falling back to shared mode. Exclusive mode needs an explicit device (`auto` is rejected at startup).

To see the device names for `audio.device`:

```sh
./tidal-tui -list-devices
```

Exclusive mode needs an `alsa/hw:CARD=<name>,DEV=<n>` device from the first list. Devices that are not USB are flagged as built-in: on some laptops (for example Apple Silicon Macs running Asahi Linux) the built-in device drives the speakers without any protection, so do not use it in exclusive mode. Shared mode takes `auto` or a `pipewire/<sink>` name from the second list.

### Bit-perfect indicator

tidal-tui compares the source format with what the sound card is actually running, read from `/proc/asound`, not just what mpv reports. The indicator has three states:

| State | Meaning |
|---|---|
| bit-perfect | Exclusive mode, lossless source, and the card runs at the source sample rate with enough bit depth (24-bit audio carried in a 32-bit format counts). |
| rate matched | Shared mode at the source rate, or a lossy (AAC) source. PipeWire may still mix or convert, so this is not claimed as bit-perfect. |
| altered | Resampled, software volume below 100%, audio filters active, bit depth reduced, or the output format cannot be verified (for example a PipeWire filter sink with no ALSA card behind it). |

The source bit depth comes from TIDAL's stream description. When it is missing, the indicator requires 24-bit output before claiming bit-perfect.

### Volume

In exclusive mode software volume is disabled: set the volume on your DAC or amplifier. Any software volume change would alter the samples. In shared mode software volume is available, and the indicator shows "altered" whenever it is below 100%.

### Gapless playback

The next track in the queue is fetched about 45 seconds before the current one ends and handed to mpv in advance. Consecutive tracks with the same format play without a gap. When the format changes (for example 44.1 kHz to 96 kHz), the device is reopened at the new rate, which leaves a short gap; tidal-tui never resamples to avoid it. Repeat-one replays the track without preloading.

### Errors

A missing mpv, a busy device, an output that cannot be opened, or an mpv crash stops playback with a message instead of hanging. If a stream breaks mid-track (usually because its signed links expired), tidal-tui fetches a fresh stream once and resumes from the same position.

## Configuration

tidal-tui reads `$XDG_CONFIG_HOME/tidal-tui/config.toml` (default `~/.config/tidal-tui/config.toml`). The file is optional and every key has a default:

```toml
# Stream quality: "aac", "lossless", or "hires".
quality = "hires"

[audio]
# "shared" plays through PipeWire. "exclusive" opens the device directly
# for bit-perfect output, blocking other apps while playing.
mode = "shared"
# mpv audio device name (see tidal-tui -list-devices). Shared mode: "auto"
# or "pipewire/<sink>". Exclusive mode: "alsa/hw:CARD=<name>,DEV=<n>".
device = "auto"

[auth]
# TIDAL developer app client ID. Overrides TIDAL_CLIENT_ID and the
# build-time default. Letters, digits, ".", "_" and "-" only.
# client_id = ""
```

Unknown keys and invalid values stop startup with an error that names the key.

The client ID is chosen in this order: `auth.client_id` in the config file, then the `TIDAL_CLIENT_ID` environment variable, then the build-time default.

Logs are written to `$XDG_STATE_HOME/tidal-tui/tidal-tui.log` (default `~/.local/state/tidal-tui/tidal-tui.log`). The directory is created with mode `0700` and the file with mode `0600`. Tokens are never logged.

## Development

| Task | Command |
|---|---|
| Run tests (with race detector) | `go test -race ./...` |
| Coverage for a package | `go test -cover ./cmd/tidal-tui` |
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
go run ./cmd/tidal-tui
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
