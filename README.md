# tidal-tui

A keyboard-driven terminal client for [TIDAL](https://tidal.com) with bit-perfect playback, built in Go on the [Charm](https://charm.sh) stack.

tidal-tui is a legitimate client: it uses only the [official TIDAL API](https://developer.tidal.com) with its own registered app, and never uses private endpoints, borrowed credentials, or DRM circumvention.

## Status

Early development. The project scaffold is in place; the UI and player are not built yet. See `PLAN.md` for the plan and decisions, and run `backlog board` for task status.

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

Release builds stamp the version at link time:

```sh
go build -ldflags "-X main.version=v0.1.0" -o tidal-tui ./cmd/tidal-tui
```

## Development

| Task | Command |
|---|---|
| Run tests (with race detector) | `go test -race ./...` |
| Coverage for a package | `go test -cover ./cmd/tidal-tui` |
| Vet | `go vet ./...` |
| Check formatting (should print nothing) | `gofmt -l .` |
| Fix formatting | `gofmt -w .` |

CI runs the formatting check, vet, race-enabled tests, and a build on every push and pull request (`.github/workflows/ci.yml`).

Tasks, docs, and decisions are tracked with [Backlog.md](https://github.com/MrLesk/Backlog.md) under `.backlog/`.

### TIDAL credentials

Developers need their own TIDAL developer app (https://developer.tidal.com) with the redirect URI `http://127.0.0.1:8765/callback`. Copy `.env.example` to `.env` and fill in the client ID. `.env` is git-ignored; never commit it.

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
