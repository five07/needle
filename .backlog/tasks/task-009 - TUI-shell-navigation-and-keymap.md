---
id: TASK-009
title: TUI shell, navigation, and keymap
status: To Do
assignee: []
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T04:28:53Z'
labels:
  - tui
milestone: m-2
dependencies:
  - TASK-003
priority: high
type: feature
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PLAN.md requires a Charm-based, keyboard-first UI with vim and arrow keys, plus mouse as a secondary input. Every view plugs into this shell.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 App renders with a sidebar (Search, Library, Discovery), a main pane, and a persistent now-playing bar
- [ ] #2 j/k/h/l and arrow keys navigate lists and panes; g/G jump to top/bottom; / focuses search; q quits
- [ ] #3 ? opens a help overlay listing all active bindings
- [ ] #4 Mouse click selects rows and switches panes; scroll wheel scrolls lists
- [ ] #5 Layout adapts to resize and stays usable at 80x24
- [ ] #6 teatest/golden tests cover navigation and resize
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
