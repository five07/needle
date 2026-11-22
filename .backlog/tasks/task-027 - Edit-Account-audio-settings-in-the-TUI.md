---
id: TASK-027
title: Edit Account audio settings in the TUI
status: To Do
assignee: []
created_date: '2026-11-22T17:46:12Z'
labels:
  - tui
  - post-mvp
dependencies:
  - TASK-014
priority: low
ordinal: 27000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Account view (TASK-014) shows the quality, audio mode, and audio device settings read-only, and they can only be changed in config.toml followed by a restart. Let the user change them from the Account view.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The quality, audio mode, and audio device can be changed from the Account view
- [ ] #2 The device choice lists the devices tidal-tui -list-devices shows for the selected mode
- [ ] #3 Changes are saved to config.toml, keeping the file's other settings and comments
- [ ] #4 Changes apply without restarting, or the view says clearly that a restart is needed
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Every acceptance criterion has at least one test that fails without the change
- [ ] #2 go test -race ./..., go vet ./..., and gofmt -l . are clean
- [ ] #3 Coverage: changed non-UI packages 95%+, TUI packages 80%+ (teatest/golden); API code has httptest coverage against a fake TIDAL server
- [ ] #4 Security self-review done (input validation, no token or internal leaks in errors, shared-state concurrency, no fail-open paths)
- [ ] #5 go build ./cmd/tidal-tui succeeds and the binary starts
- [ ] #6 README updated for behavior, config, keybinding, or tradeoff changes
- [ ] #7 Only dependencies named in the ticket added; no out-of-scope changes
- [ ] #8 slog logging to $XDG_STATE_HOME/tidal-tui/ with track_id/request_id fields; no tokens logged
<!-- DOD:END -->
