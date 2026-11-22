---
id: TASK-009
title: TUI shell, navigation, and keymap
status: Done
assignee:
  - '@claude'
created_date: '2026-11-22T03:53:35Z'
updated_date: '2026-11-22T07:11:56Z'
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
- [x] #1 App renders with a sidebar (Search, Library, Discovery), a main pane, and a persistent now-playing bar
- [x] #2 j/k/h/l and arrow keys navigate lists and panes; g/G jump to top/bottom; / focuses search; q quits
- [x] #3 ? opens a help overlay listing all active bindings
- [x] #4 Mouse click selects rows and switches panes; scroll wheel scrolls lists
- [x] #5 Layout adapts to resize and stays usable at 80x24
- [x] #6 teatest/golden tests cover navigation and resize
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
Package internal/tui on the Charm v2 stack (bubbletea, lipgloss, bubbles). Layout and keymap from UI.md; open points in PLAN.md Q21-26.
T1 Dependencies, root model, pure layout function (terminal size -> header, sidebar, main pane, now-playing bar, hint line rects), ANSI 16-color styles.
T2 Keymap registry (bubbles/key) with global/list/view scopes. One source for dispatch and the ? help overlay. Global letter keys disabled while a text input has focus.
T3 View interface with a sub-view stack (esc/backspace/h go back), generic scrollable list (j/k, arrows, g/G, ctrl+d/u), sidebar (Search, Library, Discovery, Queue, Account; 1-5 jump), placeholder views; / focuses the Search input.
T4 Now-playing bar (idle placeholder), hint line with notify slot (Q25), help overlay.
T5 Mouse: hit-testing from the layout rects; click selects rows and switches panes, wheel scrolls the list under the cursor, double-click activates.
T6 Resize and small-terminal behavior (Q22), golden tests at 80x24 and other sizes, wiring into cmd/tidal-tui (Q21), README keymap section.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented T1-T6 in internal/tui (layout.go, styles.go, keys.go, view.go, search.go, app.go, render.go) and wired the shell into cmd/tidal-tui after login (decision 21). Dependencies: charm.land/bubbletea/v2 v2.1.2, lipgloss/v2 v2.1.1, bubbles/v2 v2.2.2, github.com/charmbracelet/x/ansi (Charm stack); no teatest (decision 24).

Verification: go test -race ./... clean; internal/tui 98.2%, cmd/tidal-tui 96.4%. 10 golden screens (80x24, 120x30, 60x18 compact, 50x16 minimum, 40x12 too small, help, search input, placeholder). Every size from 20 to 160 columns and 8 to 40 rows renders exactly w x h. Mutation spot check 12/12 killed. Driven in a real pty: navigation, help, resize to compact, q exits 0, alt screen restored, mouse reporting on.

Decisions during implementation: the help overlay scrolls with j/k/g/G when it does not fit (small terminals); List rows are sanitized (escape sequences and control characters removed) so API text cannot drive the terminal; Logged-in line still printed before the UI opens.

User verified on 2026-11-22: real login, interaction in the shell, and relaunch reusing stored credentials.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added the terminal UI shell (internal/tui) on the Charm v2 stack: sidebar (Search, Library, Discovery, Queue, Account), main pane with a sub-view stack, a 6-row now-playing bar (idle until TASK-013), and a hint line with a 2-second notice slot. Keyboard per UI.md (vim and arrow keys, g/G, ctrl+d/u, /, 1-5, ?, q, ctrl+c) from one keymap that also drives the help overlay; global letters are off while the search input has focus. Mouse: click selects rows and switches panes, double-click opens, wheel scrolls the list under the pointer. Layout adapts to resize: full sidebar from 80 columns, initials strip below, a message below 50x16. The binary opens the shell after login. Verified with golden and table tests (98.2% coverage, -race, mutation checks) and in a real pty. Follow-ups: TASK-010 to 014 fill the views and bar.
<!-- SECTION:FINAL_SUMMARY:END -->
