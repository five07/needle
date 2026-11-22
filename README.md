# tidal-tui

A keyboard-driven terminal client for [TIDAL](https://tidal.com) with bit-perfect playback, built in Go on the [Charm](https://charm.sh) stack.

tidal-tui is a legitimate client: it uses only the [official TIDAL API](https://developer.tidal.com) with its own registered app, and never uses private endpoints, borrowed credentials, or DRM circumvention.

## Status

Early development. Configuration, the API client, login, and the play queue are in place; the UI and player are not built yet. See `PLAN.md` for the plan and decisions, and run `backlog board` for task status.

The TIDAL developer access tier this project currently has serves **30-second previews** only. Full-track playback needs a higher tier, which is being requested from TIDAL. Previews are full-quality DRM-free FLAC (up to 24-bit/192 kHz), so the whole playback pipeline can be developed and tested against them.

## Requirements

- **Linux** (v1 targets Linux only)
- **Go 1.27** or later to build
- **[mpv](https://mpv.io)** at runtime, used as the audio engine
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

Until the terminal UI is built, launching tidal-tui logs in if needed, prints the account name, and exits.

## Configuration

tidal-tui reads `$XDG_CONFIG_HOME/tidal-tui/config.toml` (default `~/.config/tidal-tui/config.toml`). The file is optional and every key has a default:

```toml
# Stream quality: "aac", "lossless", or "hires".
quality = "hires"

[audio]
# "shared" plays through PipeWire. "exclusive" opens the device directly
# for bit-perfect output, blocking other apps while playing.
mode = "shared"
# mpv audio device name, or "auto" for the system default.
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
