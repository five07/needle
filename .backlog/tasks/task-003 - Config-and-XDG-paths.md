---
id: TASK-003
title: Config and XDG paths
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T05:02:35Z'
labels:
  - infra
  - config
milestone: m-0
dependencies:
  - TASK-002
priority: high
type: feature
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Settings need a stable home that follows XDG conventions, and logs cannot go to stdout in a TUI. Settings decided in PLAN.md: quality aac/lossless/hires (Q6), output device and shared/exclusive mode (Q5), OAuth client ID override (Q2).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Config is read from $XDG_CONFIG_HOME/tidal-tui/config.toml with documented defaults when absent
- [x] #2 Supported keys: quality (aac, lossless, hires; default hires), audio.mode (shared, exclusive; default shared), audio.device (default system default), auth.client_id (default embedded ID)
- [x] #3 Invalid config values produce a clear error naming the key, without crashing
- [x] #4 slog logs are written to a file under $XDG_STATE_HOME/tidal-tui/
- [x] #5 Tests cover defaults, overrides, invalid values, and unset XDG variables
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 Every acceptance criterion has at least one test that fails without the change
- [x] #2 go test -race ./..., go vet ./..., and gofmt -l . are clean
- [x] #3 Coverage: changed non-UI packages 95%+, TUI packages 80%+ (teatest/golden); API code has httptest coverage against a fake TIDAL server
- [x] #4 Security self-review done (input validation, no token or internal leaks in errors, shared-state concurrency, no fail-open paths)
- [x] #5 go build ./cmd/tidal-tui succeeds and the binary starts
- [x] #6 README updated for behavior, config, keybinding, or tradeoff changes
- [x] #7 Only dependencies named in the ticket added; no out-of-scope changes
- [x] #8 slog logging to $XDG_STATE_HOME/tidal-tui/ with track_id/request_id fields; no tokens logged
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. internal/xdg: resolve config and state dirs from XDG_CONFIG_HOME / XDG_STATE_HOME, falling back to $HOME/.config and $HOME/.local/state; ignore relative XDG values per the spec; error when HOME is also unset.
2. internal/config: Config struct (quality, audio.mode, audio.device, auth.client_id) with defaults; Load(path, getenv) decodes TOML via BurntSushi/toml, treats a missing file as defaults, rejects unknown keys and invalid values with a typed *ValidationError naming the key, wraps TOML syntax errors with the file path. Client ID precedence: config file > TIDAL_CLIENT_ID env > build-time DefaultClientID.
3. internal/logging: Open(stateDir) creates $XDG_STATE_HOME/tidal-tui/ (0700) and appends to tidal-tui.log (0600) with a slog text handler.
4. Wire into cmd/tidal-tui run: resolve dirs, load config, open the log, log startup; config errors print to stderr and exit 1.
5. Table-driven tests for each package plus run integration tests using temp XDG dirs.
6. README: configuration section with keys, defaults, file locations, and the build-time client ID.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
User decisions 2026-11-21: TOML via github.com/BurntSushi/toml (approved dependency). auth.client_id default is injected at build time with -ldflags -X (nothing embedded in source until TIDAL confirms publishing is OK); TIDAL_CLIENT_ID env var overrides it for dev; config.toml overrides both.

Implemented 2026-11-21.
- internal/xdg: Resolve(getenv) for config and state dirs; relative XDG values ignored per spec; ErrNoHome when neither is usable. 100% coverage.
- internal/config: Defaults/Load/Validate; missing file = defaults; unknown keys and invalid values return *ValidationError{Key, Reason}; TOML syntax and type errors include file path, line, and key. Client ID precedence: file > TIDAL_CLIENT_ID > build-time DefaultClientID (ldflags). 96.9% coverage; 6/6 mutations killed.
- internal/logging: Open(stateDir, level) creates the dir 0700 and appends to tidal-tui.log 0600 (tightens existing files), slog text handler. 100% coverage.
- cmd/tidal-tui: run(args, getenv, stdout, stderr) resolves dirs, loads config, opens the log, logs a startup record (client_id_set only, never the ID). --version still works without HOME. 97.2% coverage.
- Manual: built binary with a temp HOME created the 0700 dir and 0600 log; invalid audio.mode exited 1 naming the key; build-time client ID not present in the log.
- Dependency: github.com/BurntSushi/toml v1.6.0, approved by the user for this ticket.
- The app does not read .env; README documents exporting it for dev runs.
<!-- SECTION:NOTES:END -->
