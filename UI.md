# UI interpretation

This is how I read the UI from `PLAN.md`, the DECISIONS table, and tasks TASK-009 to TASK-014. Nothing here is built yet. Items marked **(guess)** are my own choices where the plan is silent. Add comments in the `MS` column of any row, or under a `> MS:` marker.

## 1. Main screen at 80x24

```
┌ needle ─────────────────────────────────────────────── ? help ─ q quit ┐
│ ▸ Search      │ Search: daft punk_                                         │
│   Library     │────────────────────────────────────────────────────────────│
│   Discovery   │ TRACKS                                                     │
│   Queue (12)  │ ▶ One More Time            Daft Punk     Discovery   5:20  │
│   Account     │   Harder, Better, Faster…  Daft Punk     Discovery   3:44  │
│               │   Get Lucky                Daft Punk     Random Ac…  6:09  │
│               │ ALBUMS                                                     │
│               │   Discovery                Daft Punk     2001              │
│               │   Random Access Memories   Daft Punk     2013              │
│               │ ARTISTS                                                    │
│               │   Daft Punk                                                │
│               │ PLAYLISTS                                                  │
│               │   Daft Punk Essentials     TIDAL         40 tracks         │
├───────────────┴────────────────────────────────────────────────────────────┤
│                               ONE MORE TIME                                │
│                                                                            │
│                           Daft Punk · Discovery                            │
│              FLAC 24/96 → out 24/96 · shared · ✓ bit-perfect               │
│             1:42 ━━━━━━━━━━━━━━━━━━━●─────────────────── 5:20              │
│                       ⇄ on    [⏮]  [⏯]  [⏭]    ↻ all                       │
├────────────────────────────────────────────────────────────────────────────┤
│ enter play · a queue · A play next · f fav · R radio · S similar · ? more  │
└────────────────────────────────────────────────────────────────────────────┘
```

Everything in the now-playing bar is centered. The title is drawn at 2x size, so it takes two rows (the blank row under it is its lower half). The bar is 6 rows, which leaves 13 rows for the sidebar and main pane at 80x24.

## 2. Layout regions

| # | Region | My interpretation | Source | MS |
|---|---|---|---|---|
| 1 | Sidebar | Fixed width (about 15 cols). Entries: Search, Library, Discovery, Queue, Account, Settings (decision 70; scrolls at the minimum height, decision 73). Queue shows the upcoming count. | TASK-009 #1 | Queue and Account as sidebar entries (Q3) |
| 2 | Main pane | Shows the view selected in the sidebar. Drilling into an album/artist/playlist/mix pushes a sub-view; `esc`/`h`/`backspace` goes back **(guess)**. | TASK-009, TASK-010 #5 | |
| 3 | Now-playing bar | Always visible at the bottom, 6 rows, every line centered. Hidden or reduced to one "nothing playing" line when idle **(guess)**. | TASK-009 #1, TASK-013 | Everything centered |
| 4 | Hint line | One row of context-sensitive key hints for the focused view **(guess)**. Full list in `?` overlay. | TASK-009 #3 | |
| 5 | Header | Title plus `? help` / `q quit` **(guess)**. Could be dropped to save a row at 80x24. | none | |
| 6 | Small terminals | Below about 100 cols the sidebar collapses to icons or a single-letter column **(guess)**. At 80x24 the main pane gets about 12 list rows. | TASK-009 #5 | |

## 3. Now-playing bar

| # | Element | My interpretation | Source | MS |
|---|---|---|---|---|
| 1 | Title | Row 1 to 2, centered. 2x size using the kitty text-sizing protocol (OSC 66), with bold + color as the fallback in terminals that don't support it. Scrolls as a marquee when wider than the bar. | TASK-013 #1 | Kitty sizing with bold fallback (Q2) |
| 2 | Artist · Album | Row 3, centered, truncated with `…`. | TASK-013 #1 | Centered |
| 3 | Format | Row 4, centered. Source format from the stream (`FLAC 24/96`, `FLAC 16/44.1`, `AAC`), then output format. Green `✓ bit-perfect` when they match, yellow `⚠ resampled` when they differ. | TASK-013 #2, Decision 5 | Centered, own line |
| 4 | Output mode | On the format line: `shared` or `exclusive` **(guess)**. | Decision 5 | |
| 5 | Progress | Row 5, centered. Elapsed, bar, total. Display only, no seeking, no click-to-seek. | TASK-013 #1, Decision 8 | |
| 6 | Controls | Row 6, centered: `⇄ shuffle`, `[⏮] [⏯] [⏭]`, `↻ repeat`. All clickable (shuffle/repeat click is a **(guess)**). No rewind/seek buttons. | TASK-013 #3, #5 | |
| 7 | Preview flag | While the access tier serves 30s previews, show a `PREVIEW` badge **(guess)**. | README Status | |
| 8 | Gapless prefetch | No UI indicator **(guess)**. | Decision 13 | |

## 4. Views

| # | View | Contents | Actions in view | Source | MS |
|---|---|---|---|---|---|
| 1 | Search | Input at top. Results grouped Tracks, Albums, Artists, Playlists, about 3 to 5 per group, with "more" to expand a group **(guess)**. Debounced while typing. | Play, queue, play next, open, favorite, radio, similar | TASK-010 | |
| 2 | Album / Playlist detail | Header (name, artist/owner, year, count, duration), then track list. | Same as tracks, plus "play all" and "queue all" **(guess)** | TASK-010 #5 | |
| 3 | Artist detail | Top tracks, then albums **(guess)**. | Same as above, plus radio/similar on the artist | TASK-010 #5 | |
| 4 | Library | Sub-tabs across the top: Playlists, Albums, Tracks, Artists **(guess: tabs vs nested sidebar entries)**. Lazy paging, sorted by date added. Client-side filter. Empty states have a hint message. | Play, queue, favorite/unfavorite, add to playlist | TASK-011 | |
| 5 | Discovery | Three sections: Daily Mixes, Discovery Mixes, New Release Mixes. Empty mixes hidden. Open a mix to get a track list. | Play, queue, radio, similar | TASK-012 | |
| 6 | Radio / Similar | Result list opened from any item, shown as a sub-view in the main pane. | Same as tracks | TASK-012 #3, #4 | |
| 7 | Queue | Now playing at top, then upcoming. History hidden **(guess)**. | Jump to item, remove, clear, shuffle, repeat | TASK-013 #6, Decision 11 | |
| 8 | Account | Username, country, email, highest quality received so far. The settings moved to Settings (decision 70). | Logout | TASK-014 | |
| 8a | Settings | Visualizer on/off and theme, quality, audio mode, device (picker), autoplay. Each row changes with `enter` and is saved to config.toml after a 1 s pause; mode and device switch the engine live. | `enter` change, `esc` cancel the picker | TASK-020, TASK-027 | Decisions 70, 74-81 |
| 9 | Login | Full screen before anything else: app name, "Open this URL to log in", the URL (selectable), a spinner "waiting for browser…". | `o` re-open browser, `c` copy URL **(guess)**, `q` quit | TASK-014 #1 | |
| 10 | Add to playlist picker | Modal list of the user's playlists. Shows "already in playlist" on duplicates. | `enter` add, `esc` cancel | TASK-011 #6 | |
| 11 | Help overlay | Modal with all active bindings, grouped by global/view. | `esc`/`?` close | TASK-009 #3 | |

## 5. Keymap

Keys from the tasks are fixed. **(guess)** keys fill the gaps and avoid clashes.

| # | Key | Action | Scope | Source | MS |
|---|---|---|---|---|---|
| 1 | `j`/`k`, `↓`/`↑` | Move down/up | lists | TASK-009 | |
| 2 | `h`/`l`, `←`/`→` | Focus sidebar / main pane. In main pane, `h` also goes back from a sub-view **(guess)** | global | TASK-009 | |
| 3 | `g` / `G` | Top / bottom | lists | TASK-009 | |
| 4 | `ctrl+d` / `ctrl+u` | Half page down/up **(guess)** | lists | none | |
| 5 | `tab` / `shift+tab` | Next/previous Library sub-tab or Search group **(guess)** | view | none | |
| 6 | `/` | Go to Search and focus input. In Library it filters the current list instead | global | TASK-009, TASK-011 #3 | Approved (Q1) |
| 7 | `enter` | Play (track) or open (album/artist/playlist/mix) | lists | TASK-010 #4 | |
| 8 | `esc` / `backspace` | Back / close overlay / leave input | global | **(guess)** | |
| 9 | `a` | Add to queue | items | **(guess)** | |
| 10 | `A` | Play next | items | **(guess)** | |
| 11 | `f` | Favorite / unfavorite | items | **(guess)** | |
| 12 | `+` | Add track to playlist | tracks | **(guess)** | |
| 13 | `R` | Start radio | track, artist | **(guess)** | |
| 14 | `S` | Similar | track, artist, album | **(guess)** | |
| 15 | `space` | Play / pause | global | TASK-013 | |
| 16 | `n` / `]` | Next track | global | TASK-013 | |
| 17 | `p` / `[` | Previous track (restarts if at first track) | global | TASK-013, TASK-006 | |
| 18 | `s` | Toggle shuffle | global | TASK-013 | |
| 19 | `r` | Cycle repeat off/all/one | global | TASK-013 | |
| 20 | `d` / `x` | Remove from queue | Queue view | **(guess)** | |
| 21 | `D` | Clear queue (with confirm) | Queue view | **(guess)** | |
| 22 | `1`..`5` | Jump to sidebar entry **(guess)** | global | none | |
| 23 | `?` | Help overlay | global | TASK-009 | |
| 24 | `q` / `ctrl+c` | Quit (`q` types normally while in the search input) | global | TASK-009 | |
| 25 | `-` / `=` | Volume down / up by 5% (software volume; a notice shows the level) | global | TASK-018 | Approved (Q82) |

Global letter keys (`space`, `n`, `p`, `s`, `r`, `q`) are disabled while a text input has focus, so search text can include them. `esc` leaves the input.

## 6. Mouse

| # | Interaction | Result | Source | MS |
|---|---|---|---|---|
| 1 | Click sidebar entry | Switch view | TASK-009 #4 | |
| 2 | Click row | Select it | TASK-009 #4 | |
| 3 | Double-click row | Play / open **(guess)** | none | |
| 4 | Scroll wheel | Scroll the list under the cursor | TASK-009 #4 | |
| 5 | Click transport buttons | Prev / play-pause / next | TASK-013 #5 | |
| 6 | Click shuffle / repeat | Toggle **(guess)** | none | |
| 7 | Right-click row | Action menu (queue, play next, fav, radio, similar, add to playlist) **(guess)** | none | |
| 8 | Click progress bar | Nothing (no seek in v1) | Decision 8 | |

## 7. Feedback and states

| # | Situation | Display | MS |
|---|---|---|---|
| 1 | Loading a view | Spinner in the main pane **(guess)** | |
| 2 | Action done (queued, favorited) | Short toast in the hint line for about 2s **(guess)** | |
| 3 | API error | Toast with a plain message, details only in the log file | |
| 4 | Network down / token expired | Banner above the bar; token expiry sends you to the login screen **(guess)** | |
| 5 | mpv missing | Startup error before the TUI opens **(guess)** | |
| 6 | Queue end, repeat off | Bar shows "stopped", last track info stays **(guess)** | |

## 8. Theming

| # | Item | My interpretation | MS |
|---|---|---|---|
| 1 | Colors | Use the terminal's 16-color palette (so Omarchy themes apply) rather than hardcoded hex **(guess)** | |
| 2 | Focus | Focused pane has a highlighted border; selected row is reverse video | |
| 3 | Config | No theme config in v1 **(guess)** | |

## 9. Open points (answered)

1. `/` is "focus search" (TASK-009) but Library also needs a filter key (TASK-011 #3). I propose `/` filters inside Library and jumps to Search everywhere else. Alternative: a separate filter key such as `F`.
   > MS: go with your proposal
2. The 2x title: kitty text sizing (kitty/ghostty only) with bold fallback, or block-letter big text in every terminal? Big text costs 2 to 3 extra rows, which is tight at 80x24.
   > MS: agreed
3. Should Queue and Account be sidebar entries (as drawn) or overlays opened by a key?
   > MS: as drawn
