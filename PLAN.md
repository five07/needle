# PLAN

`tidal-tui` is a Go based Tidal tui client.

- It will utilize the well documented Tidal API for bit-perfect playback
- It will utilize the Charm framework for an easy and clean look
- It will have the following features:
  - User login and account info storage
  - search
  - User's playlists, albums, etc
  - Discovery
  - Player with play, pause, rewind, advance
  - show current track info (title, artist, bit-depth/quality)
  - no album art at this time
  - search functions can be used during play
  - add to queue with "Play next" and "Add to queue" options
  - keyboard driven by default with vim-style and arrow-key integration
  - mouse interation as a secondary option.

## QUESTIONS

Answer inline after each `> MS:` marker. Anything left blank takes the recommended default.

API facts behind these questions are in backlog doc `doc-001` (TIDAL API third-party access reference), taken from the official OpenAPI spec v1.10.258.

1. **Tidal API access.** The official API (`openapi.tidal.com/v2`) gives THIRD_PARTY apps `GET /trackManifests/{id}` with format `FLAC_HIRES`. The spec also shows three risks:
   - The response can carry Widevine/FairPlay `drmData`, which mpv cannot decrypt.
   - It can serve a 30s preview with reason `FULL_REQUIRES_HIGHER_ACCESS_TIER`.
   - It requires scope `r_usr`, which the spec marks as INTERNAL tier.

   The fallback is the unofficial private API (`api.tidal.com/v1`, device-code login with a borrowed client ID). It works today, but it breaks without notice and is a ToS gray area.
   - a) Official API only. If full, DRM-free manifests are not available, the project blocks.
   - b) Unofficial API only.
   - c) **(Recommended)** Official API first, behind a `Provider` interface. Spike ticket TASK-001 first checks for full tracks, no DRM, and the `r_usr` grant, and that result decides whether to fall back to (b) for playback only.
   > MS: c. 

   _Clarified in chat (2026-11-21): use the official API only, with no unofficial fallback of any kind. DRM gets dealt with if and when we hit it. Recorded as backlog `decision-001`._

2. **OAuth client ID and login flow.** The official API offers only authorization code + PKCE (needs a browser and a redirect URI) and client credentials (needs a secret). It has no device-code flow. An OSS app cannot keep a secret.
   - a) **(Recommended)** Register one public PKCE client for the project and embed its client ID, with a config override. Login opens the browser and catches the redirect on a loopback listener (`http://127.0.0.1:<port>/callback`), printing the URL for headless use. Whether TIDAL accepts loopback redirects is unverified, so TASK-001 checks it.
   - b) Require each user to register their own app at developer.tidal.com and paste in a client ID.
   > MS: a

3. **Audio engine.** Hi-res Tidal streams are DASH/HLS manifests with FLAC in fragmented MP4.
   - a) **(Recommended)** Drive `mpv` as a subprocess over JSON IPC. It handles DASH and FLAC, supports bit-perfect output (`--audio-exclusive`, ALSA `hw:` devices, PipeWire), reports the real output format, and supports gapless playback. Cost: mpv becomes a runtime dependency.
   - b) Pure Go: write the manifest/fMP4 demuxing and FLAC decoding in Go, with ALSA/PipeWire output via cgo. No runtime dependency, but this is several weeks of work and the hardest part to get bit-perfect.
   - c) libmpv via cgo. Same capabilities as (a), but builds need cgo and you get a single binary.
   > MS: a

4. **Target platforms for v1.**
   - a) **(Recommended)** Linux first (PipeWire and ALSA), with code kept portable so macOS builds compile.
   - b) Linux and macOS, both fully supported.
   - c) Linux, macOS, and Windows.
   > MS: a, v1 is linux only

5. **Meaning of "bit-perfect" and the output device.**
   - a) **(Recommended)** Output device is configurable. The default is shared PipeWire output with sample-rate switching. An "exclusive" mode opens an ALSA `hw:` device directly, which blocks other apps' audio. The now-playing bar shows both the source format and the actual output format, so any resampling is visible.
   - b) Exclusive ALSA only. Every track is guaranteed bit-perfect, but no other app can play sound at the same time.
   - c) Shared output only. Show the source quality and stop there.
   > MS: a

6. **Stream quality.** The API takes a list of acceptable formats (`HEAACV1`, `AACLC`, `FLAC`, `FLAC_HIRES`, `EAC3_JOC` for Dolby Atmos) and reports which ones the manifest contains. It does not report bit depth or sample rate, so those come from the stream itself.
   - a) **(Recommended)** Add a quality setting (`aac`, `lossless`, `hires`), defaulting to `hires`, which maps to `[FLAC_HIRES, FLAC, AACLC]`, so the server falls back automatically. Show the received format with its bit depth and sample rate. Leave Dolby Atmos (`EAC3_JOC`) out, since it can't be bit-perfect stereo.
   - b) Always request `[FLAC_HIRES, FLAC, AACLC]`, with no setting.
   > MS: a

7. **Credential storage and accounts.** "Account info" from `GET /users/me` is `username`, `country`, `email`, and name. There is no subscription tier or maximum quality field.
   - a) **(Recommended)** Store tokens in the OS keyring (Secret Service on Linux) and fall back to a `0600` file under `$XDG_CONFIG_HOME/tidal-tui/` when no keyring is available. One account at a time.
   - b) `0600` file only.
   - c) Keyring, and support multiple accounts you can switch between.
   > MS: a

8. **Rewind and advance.** Do these mean seeking or changing tracks?
   - a) **(Recommended)** Support both. Seek -/+10s (`h`/`l`, `←`/`→`, configurable step). Previous/next track (`p`/`n`, or `[`/`]`). "Previous" restarts the current track when more than 3s in.
   - b) Previous/next track only.
   - c) Seek only.
   > MS: no seek in v1, only prev/next track. I'm not sure how a scrubber would work in a tui.

9. **Contents of Discovery.** THIRD_PARTY can access Daily Mixes, Discovery Mixes, New Release Mixes, track/artist radio, and similar tracks/artists/albums. The home feed, genres, and editorial pages are INTERNAL only.
   - a) **(Recommended)** A Discovery view with Daily, Discovery, and New Release mixes. Add "Start radio" and "Similar" actions on any track, artist, or album, available from every view.
   - b) Mixes only.
   > MS: a

10. **Library write actions.** All of these are available to THIRD_PARTY (`collection.write`, `playlists.write`). Library text search is not, so filtering a library view happens client-side.
    - a) Read-only library browsing.
    - b) **(Recommended)** Read-only browsing, plus favorite/unfavorite (track, album, artist, playlist) and "add track to playlist".
    - c) Full playlist management: everything in (b), plus create, rename, reorder, remove items, and delete.
    > MS: b

11. **Queue extras beyond Play next / Add to queue.**
    - a) **(Recommended)** Shuffle, repeat (off/all/one), remove from queue, and clear queue. Stop at the end of the queue. Queue persistence and autoplay radio go to the backlog for later.
    - b) Only what PLAN.md lists.
    - c) Everything in (a), plus queue persistence across restarts and autoplay radio at the end of the queue.
    > MS: a

12. **Desktop integration (MPRIS).** This would make media keys and Waybar/omarchy-shell widgets work.
    - a) **(Recommended)** Include it in v1 as its own ticket, after the player works.
    - b) Defer until after v1.
    > MS: a

13. **Gapless playback.**
    - a) **(Recommended)** Best-effort gapless in v1: prefetch the next queue item's manifest and use mpv's gapless handling.
    - b) Not needed for v1.
    > MS: a

14. **Adapting the Definition of Done.** The CLAUDE.md template targets an HTTP server: `go build ./cmd/server`, `httptest` against real handlers, an order ID log field, and "no new dependencies".
    - a) **(Recommended)** Copy the DoD into PLAN.md with these changes:
      - Build `./cmd/tidal-tui`.
      - `httptest` runs against a fake Tidal API server, and that is the e2e layer.
      - Use slog fields `track_id`/`request_id`, logging to a file under `$XDG_STATE_HOME`.
      - Dependencies are allowed when a ticket names them (Charm v2 stack, keyring, and so on).
      - 95% coverage for non-UI packages and 80% for TUI packages, which use `teatest`/golden tests.
      - Mirror the DoD into backlog's project `definition_of_done`.
    - b) Keep the template as-is and handle exceptions ticket by ticket.
    > MS: a

15. **License and module path.**
    - a) **(Recommended)** MIT, with module path `github.com/five07/tidal-tui` (taken from the git remote).
    - b) Apache-2.0, with the same module path.
    - c) GPL-3.0, with the same module path.
    > MS: a

### TASK-008 (audio engine)

Background for these questions is in backlog doc `doc-003` (audio spike with the Schiit Vestri).

16. **What "bit-perfect" means in shared mode.** In the spike, PipeWire switched the DAC to the source rate when it was idle, but resampled to 48 kHz when another app was already playing, and mpv could not see that. tidal-tui can read the real hardware rate from the card in both modes. PipeWire still mixes in floating point, so shared output cannot be proven bit-perfect.
    - a) **(Recommended)** Three states. "Bit-perfect" only in exclusive mode, when the hardware rate equals the source rate, the format is lossless, there are no filters, and software volume is 100%. "Rate matched" in shared mode when the hardware rate equals the source rate. "Resampled" whenever the rates differ or the output rate is unknown.
    - b) Two states. Shared mode counts as bit-perfect when the rates match and volume is 100%.
    - c) Two states. Shared mode is never bit-perfect, with no "rate matched" state.
    > MS: a

17. **Exclusive mode when the DAC is busy.** If another app is playing through PipeWire, mpv fails to open the `hw:` device ("Device or resource busy").
    - a) **(Recommended)** Show a clear error ("the DAC is in use by another app; stop it or switch to shared mode") and do not play. Never fall back to shared output silently, because the user asked for exclusive.
    - b) Fall back to shared mode automatically, with the indicator off and a notice.
    - c) Ask PipeWire to release the device through the D-Bus `org.freedesktop.ReserveDevice1` handshake, then open it. This makes `github.com/godbus/dbus/v5` (already an indirect dependency via go-keyring) a direct dependency.
    > MS: a

18. **Source bit depth.** mpv reports 24-bit FLAC as `s32` (its container), not 24. TIDAL does not document bit depth anywhere.
    - a) **(Recommended)** Read it from the manifest's representation id (observed as `FLAC_HIRES,192000,24`). If it doesn't parse, show the sample rate with no bit depth. No extra requests.
    - b) Read the FLAC header from the stream's initialization segment. This is reliable but costs an extra signed request per track and an MP4 box parser.
    - c) Don't show bit depth; show codec and sample rate only.
    > MS: a

19. **Choosing the exclusive-mode device.** `audio.device = "auto"` has no safe meaning in exclusive mode. On this MacBook, the first ALSA playback device is the raw speaker path, which skips the speaker-safety processing.
    - a) **(Recommended)** Exclusive mode requires an explicit ALSA device (for example `alsa/hw:CARD=Vestri,DEV=0`); `auto` with exclusive is a config error that names the key. Add `tidal-tui -list-devices` to print the available output devices.
    - b) Pick the first USB audio device automatically, and error if there is none.
    - c) Same as (a), without `-list-devices` (users run `mpv --audio-device=help` instead).
    > MS: a

20. **Software volume.** Any software volume below 100% changes the samples.
    - a) **(Recommended)** Exclusive mode disables software volume: volume keys do nothing, and the UI shows that volume is set on the DAC or amp. Shared mode allows volume, and the indicator shows "rate matched" only at 100%.
    - b) Allow volume in both modes, and turn the indicator off below 100%.
    - c) No software volume in either mode for v1.
    > MS: a, but tag a post-MVP that allow the user to choose "volume on your output device" or "allow this computer volume to also control volume" - something like that

### TASK-009 (TUI shell)

Layout and keymap follow `UI.md`. These are only the points where `UI.md` is silent, contradicts itself, or meets the New Items list.

21. **What the binary does after TASK-009.** Today `tidal-tui` logs in, prints the account name, and exits.
    - a) **(Recommended)** After login, open the TUI shell. Views are placeholders until their tickets land (Search in TASK-010, and so on), and the now-playing bar shows "Nothing playing" until TASK-013 connects the player. `q` quits.
    - b) Keep the current behavior. The shell is exercised only by tests until TASK-014 wires it in.
    > MS: a

22. **Narrow terminals.** The `UI.md` drawing shows the full 15-column sidebar at 80 columns, but region 6 guesses the sidebar collapses below about 100 columns.
    - a) **(Recommended)** Full sidebar at 80 columns and wider, as drawn. Below 80 columns it collapses to a 3-column letter strip (`S L D Q A`). Below 50x16 the app shows "Terminal too small (needs 50x16)" in place of the layout.
    - b) Collapse the sidebar below 100 columns, as region 6 says, giving the main pane the space at 80x24.
    > MS: a

23. **Now-playing bar when idle.** The bar takes 6 rows, leaving 13 rows for the lists at 80x24.
    - a) **(Recommended)** Always 6 rows, with "Nothing playing" centered when idle. The layout never jumps when playback starts or stops.
    - b) Shrink to one row when idle, so lists get 5 more rows until something plays.
    > MS: a

24. **Test tooling.** The ticket asks for teatest/golden tests. The Charm v2 stack has releases (`charm.land/bubbletea/v2` v2.1.2, `lipgloss/v2` v2.1.1, `bubbles/v2` v2.2.2), but teatest v2 exists only as unreleased commits (`github.com/charmbracelet/x/exp/teatest/v2`, newest dated 2026-11-22).
    - a) **(Recommended)** Drive the Bubble Tea model directly in tests (send key, mouse, and resize messages to `Update`, compare `View` output) with a small in-repo golden-file helper (`go test -update`). No test-only dependency.
    - b) Use teatest v2, pinned to an older commit.
    > MS: a

25. **Notifications (New Items 1).** `UI.md` section 7 puts short messages in the hint line for about 2 seconds. New Items 1 wants a cliamp-style notice for volume and transport changes.
    - a) **(Recommended)** Build the notice slot in TASK-009: a generic notify call that shows text in the hint line for 2 seconds, then restores the hints. TASK-013 sends volume and transport notices through it.
    - b) Leave notices to TASK-013.
    > MS: a

26. **Arrow-key scrubbing (New Items 2) and post-MVP items (New Items 3 and 4).** TASK-009 #2 makes `←`/`→` switch panes, and decision 8 says no seeking in v1, so arrow-key scrubbing would clash with both.
    - a) **(Recommended)** Keep `←`/`→` for panes in v1. Create post-MVP backlog tasks for scrubbing (with a key to be chosen then, such as `,`/`.`), the visualizer toggle (New Items 3), and EQ with presets (New Items 4).
    - b) Keep `←`/`→` for panes, and leave the New Items for a later planning pass.
    - c) Reserve `←`/`→` for seeking now. Panes then switch with `h`/`l` and `tab` only, which changes TASK-009 #2.
    > MS: a

### TASK-010 (search)

27. **Playing for real.** AC #4 says Enter plays. The player (TASK-008) and queue (TASK-006) exist, but `cmd/tidal-tui` does not start them yet, and TASK-013 (bar and transport keys) does not mention wiring them.
    - a) **(Recommended)** TASK-010 wires queue, resolver, and player into `cmd/tidal-tui` behind a small `tui.Playback` interface (play a list from an index, play next, add to queue). Enter really plays, and a notice in the hint line confirms each action ("Playing ...", "Queued ..."). The bar stays idle until TASK-013.
    - b) TASK-010 defines the interface and tests it with a stub only. The binary gets a no-op that shows "Playback arrives in TASK-013", and TASK-013 does the wiring.
    > MS: a

28. **What Enter on a track queues.**
    - a) **(Recommended)** The list it sits in, starting at that track: in search, the Tracks group; in an album or playlist, the whole track list (loaded pages). This matches how album playback usually works.
    - b) Only the selected track, replacing the queue, everywhere.
    - c) Only the selected track in search results; the whole list from that track in album and playlist views.
    > MS: a

29. **Play next / Add to queue on an album, artist, or playlist row.**
    - a) **(Recommended)** Album and playlist rows fetch their tracks and queue all of them (in order, capped at 500 tracks with a notice when capped). On an artist row the keys do nothing and the hint shows "open the artist to queue tracks".
    - b) Queue keys work on track rows only, everywhere.
    - c) Like (a), and on an artist row queue the artist's top tracks.
    > MS: a

30. **How many results per group.** `searchResults` relationships return 20 items per page.
    - a) **(Recommended)** Show the first page of each group (up to 20) in one grouped, scrolling list. `tab`/`shift+tab` jump to the next/previous group header. No "more" expansion in v1.
    - b) Show 5 per group with a "more ..." row that expands the group in place and pages further on demand.
    > MS: a

### TASK-011 (library)

API facts below come from the official OpenAPI spec v1.10.258 (the same version as doc-001).

31. **Knowing what is already a favorite.** AC #5 wants favorite/unfavorite from search results too. The API has no "is this a favorite?" call: collection items can only be listed (20 per page) or searched with `filter[self][matchesQuery]`, which is INTERNAL tier.
    - a) **(Recommended)** Two keys, no extra requests: `f` adds to favorites, `F` removes, in every view. A row shows `♥` when tidal-tui knows it is a favorite (the item is in a loaded Library list, or you favorited it this session). Removing in a Library view drops the row.
    - b) One toggle key `f`. At startup tidal-tui loads the IDs of every favorite track, album, artist, and playlist in the background (one request per 20 items, so 100 requests for 2000 tracks), and `f` waits for that list before acting on an unknown item.
    - c) One toggle key `f` that only knows items loaded in the Library this session. On anything else it adds.
    > MS: a

32. **What the Playlists tab lists.** TIDAL has two lists: playlists you own (`/playlists?filter[owners.id]=me`) and playlists in your collection (`/userCollectionPlaylists/me`, which per the spec can include folders). The probe below shows whether the collection already contains your own playlists.
    - a) **(Recommended)** The Playlists tab shows your collection (owned and favorited, flat, no folders), newest first. "Add to playlist" offers only playlists you own, since only those can be edited.
    - b) Two tabs: "My playlists" and "Favorite playlists".
    > MS: a

33. **Adding a track that is already in the playlist.** The API takes `onDuplicates` = `ADD`, `FAIL`, or `SKIP`.
    - a) **(Recommended)** Use `FAIL`, and show "Already in <playlist>" without adding it. There is no "add anyway" in v1.
    - b) Report the duplicate and offer `y` to add it anyway (a second request with `ADD`).
    - c) Always add (`ADD`) and say nothing.
    > MS: a

34. **Library layout.** `UI.md` section 4 marks the tab layout as a guess.
    - a) **(Recommended)** One Library section with sub-tabs across the top: Playlists, Albums, Tracks, Artists. `tab`/`shift+tab` switch tabs (there are no result groups here), and a click on a tab name selects it. Each tab loads on first visit.
    - b) Separate sidebar entries for each list, which makes the sidebar longer and changes the `1`-`5` keys.
    > MS: a

35. **How far the filter reaches.** AC #3 says the filter narrows the loaded items. With 20 items per page, that can be a small part of a large library.
    - a) **(Recommended)** While a filter is active, tidal-tui keeps loading the remaining pages in the background (up to 5000 items per tab, with a notice at the cap), so the filter covers the whole list. It matches title, artist, and album, ignoring case and accents.
    - b) Filter only what has been loaded by scrolling.
    > MS: a

36. **Sort order.** AC #2 says sorted by date added "by default".
    - a) **(Recommended)** Newest added first, plus an `o` key that cycles the server-side sort for the current tab (date added, then title, then artist name), reloading the list.
    - b) Newest added first only, with no sort key in v1.
    > MS: a

### TASK-012 (discovery)

API facts below come from the official OpenAPI spec v1.10.258 and a live probe on 2026-11-22. Mixes and radios are ordinary `playlists` resources with `playlistType: "MIX"` and a `description` (for example "Julian Casablancas, Gorillaz, MGMT and more") but no track count. Their tracks load through the same call as any playlist. The probe returned 8 Daily Mixes, 1 Discovery Mix, and 1 New Release Mix, with no paging. Track and artist radio each return one mix. There is no radio for albums or playlists, and no "similar" for playlists. Similar tracks, artists, and albums come 20 per page.

40. **Discovery layout.** `UI.md` section 4 lists three sections: Daily Mixes, Discovery Mixes, New Release Mixes.
    - a) **(Recommended)** One grouped list, like Search results: a heading per group, `tab`/`shift+tab` jump between groups, and groups with no mixes are left out. Each mix row shows its name and description.
    - b) Sub-tabs, like the Library.
    > MS: b. in fact I prefer that layout, let's create a task to do that in search too, including the help bar update.

41. **When mixes reload.** TIDAL updates Daily and Discovery mixes every morning, so a list loaded at startup goes stale in a long session.
    - a) **(Recommended)** Load when Discovery is first shown, and load again when you come back to it more than an hour later. No refresh key.
    - b) Load once per session.
    - c) Load once, plus a refresh key.
    > MS: a

42. **Favorites and "add to playlist" on mixes.** Mixes are playlists, but they are generated for you and are not in the Library collection. Whether TIDAL accepts a mix as a favorite is unverified.
    - a) **(Recommended)** `f`/`F` on a mix row say "Mixes cannot be added to favorites" and send nothing. Tracks inside a mix work as everywhere else.
    - b) Send the request anyway and show TIDAL's answer.
    > MS: a. I just tested it and they can be favorited

43. **Radio and Similar keys.** `UI.md` section 5 marks `R` (radio) and `S` (similar) as guesses. `s` and `r` are taken by shuffle and repeat in TASK-013.
    - a) **(Recommended)** `R` starts radio on a track or artist and `S` lists similar items for a track, artist, or album, in every view. On a row without one (radio on an album, playlist, or mix; similar on a playlist or mix) a notice says what is supported.
    - b) Other keys (name them).
    > MS: a

### TASK-024 (search sub-tabs)

44. **Which keys lead the Search hint line.** AC #3 asks for the most important Search keys first, including `tab`, like the Library's `/ filter · tab next tab`.
    - a) **(Recommended)** `/ search · tab next tab` first, then `j/k move · h back` and the item keys. With the sidebar focused on Search, `/ search` also moves to the front, as `/ filter` does for the Library.
    - b) Only `tab next tab` first; `/ search` stays near the end.
    > MS: a

45. **`tab` while typing in the search input.** Today the input takes `tab` and does nothing with it, so you must press `esc` before switching tabs.
    - a) **(Recommended)** `tab` and `shift+tab` in the input leave it and switch to the next / previous tab, so you can type a query and go straight to Albums.
    - b) `tab` in the input only leaves it (like `esc`); a second `tab` switches.
    - c) Unchanged: `esc` first.
    > MS: a

46. **The artist view.** It still shows Top tracks and Albums as one grouped list with headings (TASK-010).
    - a) **(Recommended)** Leave it in this task; add a backlog task to move it to sub-tabs too.
    - b) Move it to sub-tabs in this task.
    > MS: a

### TASK-013 (now-playing bar)

47. **The 2x title (OSC 66).** The TUI renderer, Charm's cell buffer, drops every OSC escape except hyperlinks, so a 2x title can't be drawn as part of the normal frame. It would have to be written to the terminal outside the frame after each redraw, with cursor moves. That path may flicker or get erased when the renderer repaints the row. Also, only kitty implements OSC 66 as far as I know. Your terminal is Ghostty, which I don't believe supports it yet, so you would see the bold fallback.
    - a) **(Recommended)** Bold title on 2 rows now (the blank second row keeps the layout from UI.md). Split the real 2x title into its own backlog task, which starts with a spike in kitty. AC #1 is checked against the bold fallback.
    - b) Try it in this task: write the title out of frame, only when kitty is detected (`TERM=xterm-kitty` or `KITTY_WINDOW_ID`), bold everywhere else. Drop it if the spike shows flicker.
    > MS: a

48. **What counts as "the track that is currently playing" for enter (AC #8).**
    - a) **(Recommended)** The same track ID as the current track, in any list. Enter on it toggles pause, even if you started playback from a different album or playlist.
    - b) Only when the queue was started from this same list; in another list, enter replaces the queue as today.
    > MS: a

49. **Clearing the queue (AC #6).** `D` clears the upcoming tracks (the current track keeps playing). `d`/`x` remove the selected upcoming track.
    - a) **(Recommended)** `D` asks first: a notice "Press D again to clear the queue" and a second `D` within 2 s clears it.
    - b) `D` clears at once, with a notice saying how many tracks were removed.
    > MS: a

### TASK-014 (account view)

The scope was simplified: login stays in the terminal before the TUI starts (it already works that way), and logout stays the `-logout` flag. Questions 50, 51, and 54 no longer apply and were removed.

52. **"Highest quality received so far" (AC #2).**
    - a) **(Recommended)** Remembered across runs in a small file under `$XDG_STATE_HOME/tidal-tui/`, so it says what your account can get (for example "FLAC 24/192, first seen 2026-11-22"). `tidal-tui -logout` forgets it.
    - b) This session only; it says "nothing played yet" until the first track starts.
    > MS: a

53. **Other lines in the Account view.** UI.md guessed at the audio settings here.
    - a) **(Recommended)** Also show the `quality` setting, the audio mode, and the audio device, read-only, with a hint that they are set in `config.toml`.
    - b) Only the AC fields: username, country, email, and highest quality.
    > MS: a

### Rename to `needle`

The product name `tidal-tui` puts TIDAL's trademark in the name, so the project becomes `needle`. Describing it as "a terminal client for TIDAL" stays, since that only names the service it works with. Checked on 2026-11-22: there is no `needle` package in the AUR or the Arch repos (`needle-cli`, an unrelated GitHub PR tool, is the closest), and `github.com/five07/needle` is free.

Scope (`tidal-tui` appears in about 50 files):

- Go module path `github.com/five07/tidal-tui` becomes `github.com/five07/needle`, including every import.
- `cmd/tidal-tui/` becomes `cmd/needle/`, the binary becomes `needle`, and `.gitignore` follows.
- Runtime names: `$XDG_CONFIG_HOME/needle/`, `$XDG_STATE_HOME/needle/needle.log`, `$XDG_RUNTIME_DIR/needle/`, the `needle-` temp directory prefix, keyring service `needle`, and the login callback page text.
- The TUI frame title, and with it every golden file under `internal/tui/testdata/`.
- README, UI.md, the DoD in this file, and the backlog project name and Definition of Done.
- Unchanged: `TIDAL_CLIENT_ID`, the `tidal` package, and the keyring account key `tidal-account`, since these describe the TIDAL service, not the product.

These steps belong to you, because they are git or account actions:

- Rename the GitHub repo to `five07/needle` (GitHub redirects the old URL) and update the `origin` remote.
- Rename the app at developer.tidal.com if its display name says `tidal-tui`. The redirect URI `http://127.0.0.1:8765/callback` does not change.

37. **Existing config, credentials, and logs.** After the rename, `needle` looks in new directories and a new keyring entry, so an existing login and config are not found.
    - a) **(Recommended)** No migration. The project has no releases, so the README says nothing about it, and you run `tidal-tui -logout` once before switching, then move `config.toml` by hand if you have one.
    - b) A one-time migration at startup: if `needle` finds no config or keyring entry but the old ones exist, it moves the config directory, copies the keyring entry to the new service and deletes the old one, and logs that it did so. Removed again after the first release.
    > MS: a

38. **When the rename lands.** TASK-011 (Library) is in progress with uncommitted changes, and the rename touches every package that TASK-011 also touches.
    - a) **(Recommended)** As its own ticket right after TASK-011 is committed, before TASK-012 starts, so each diff stays reviewable.
    - b) Now, before TASK-011 continues.
    - c) After v1 is feature-complete.
    > MS: c

39. **Affiliation notice.** The README would describe `needle` as a client for TIDAL.
    - a) **(Recommended)** Add one line under the README intro: "needle is an independent project and is not affiliated with or endorsed by TIDAL." Also print it in `-version` output.
    - b) README line only.
    - c) No notice.
    > MS: a

## DECISIONS

These are the answers to the questions above. Backlog tasks are written to match them.

| # | Topic | Decision |
|---|---|---|
| 1 | API | Official TIDAL API only, with no unofficial fallback (`decision-001`). DRM gets handled if TASK-001 finds it. |
| 2 | Login | One public PKCE client ID embedded, with a config override. Browser login with a loopback redirect, and the URL also printed. |
| 3 | Audio engine | `mpv` subprocess controlled over JSON IPC. |
| 4 | Platforms | Linux only for v1. |
| 5 | Output | Configurable device. Default is shared PipeWire. Optional exclusive ALSA `hw:` mode. The UI shows source vs output format. |
| 6 | Quality | Setting `aac` / `lossless` / `hires` (default `hires`), mapping to `[AACLC]`, `[FLAC, AACLC]`, `[FLAC_HIRES, FLAC, AACLC]`. Dolby Atmos excluded. |
| 7 | Credentials | OS keyring (Secret Service), with a `0600` file fallback. One account. |
| 8 | Transport | Previous/next track only. No seeking in v1. |
| 9 | Discovery | Daily, Discovery, and New Release mixes, plus "Start radio" and "Similar" actions on tracks, artists, and albums. |
| 10 | Library writes | Favorite/unfavorite (track, album, artist, playlist) and "add track to playlist". |
| 11 | Queue | Play next, add to queue, shuffle, repeat (off/all/one), remove, and clear. Stops at the end. Persistence and autoplay radio come later. |
| 12 | MPRIS | In v1, after the player works. |
| 13 | Gapless | Best effort: prefetch the next manifest and use mpv gapless. |
| 14 | DoD | Adapted copy below, mirrored into backlog's project Definition of Done. |
| 15 | License | MIT, `github.com/five07/tidal-tui`. |
| 16 | Bit-perfect indicator | Three states: "bit-perfect" (exclusive only: hardware rate equals source, lossless format, no filters, volume 100%), "rate matched" (shared mode at the source rate), "resampled" (rates differ or output rate unknown). |
| 17 | Busy DAC in exclusive mode | Clear error, no playback, no silent fallback to shared. |
| 18 | Source bit depth | From the manifest representation id (`FLAC_HIRES,192000,24`); show the sample rate alone if it doesn't parse. |
| 19 | Exclusive device | Must be an explicit ALSA device; `auto` with exclusive is a config error. Add `tidal-tui -list-devices`. |
| 20 | Software volume | Disabled in exclusive mode (UI says volume is on the DAC/amp); allowed in shared mode, "rate matched" only at 100%. A post-MVP setting to choose device or software volume is TASK-018. |
| 21 | Binary after TASK-009 | After login, open the TUI shell. Views are placeholders until their tickets land; the bar shows "Nothing playing" until TASK-013. |
| 22 | Narrow terminals | Full sidebar at 80 columns and wider; a 3-column letter strip below 80; "Terminal too small (needs 50x16)" below 50x16. |
| 23 | Idle now-playing bar | Always 6 rows, "Nothing playing" centered when idle. |
| 24 | TUI tests | Drive the Bubble Tea model directly with an in-repo golden helper (`go test -update`). No teatest dependency. |
| 25 | Notifications | Notice slot in the hint line (2 seconds) built in TASK-009; TASK-013 sends volume and transport notices. |
| 26 | Arrow keys and New Items 2-4 | `←`/`→` switch panes in v1. Post-MVP tasks for scrubbing, a visualizer toggle, and EQ with presets. |
| 27 | Search playback | TASK-010 wires queue, resolver, and player into `cmd/tidal-tui` behind `tui.Playback`; Enter really plays, with a notice per action. The bar stays idle until TASK-013. |
| 28 | Enter on a track | Queues the list it sits in, starting at that track (search Tracks group, or the loaded album/playlist list). |
| 29 | Queue keys on collection rows | Album and playlist rows queue all their tracks (capped at 500, with a notice). Artist rows do nothing and hint to open the artist. |
| 30 | Results per group | First page per group (up to 20) in one grouped list; `tab`/`shift+tab` jump between groups. No "more" expansion in v1. |
| 31 | Favorite state | `f` adds to favorites, `F` removes, in every view; `♥` marks items known to be favorites (loaded in Library, or favorited this session). Removing in Library drops the row. No extra requests. |
| 32 | Playlists tab | Shows the collection (owned and favorited, flat, no folders), newest first. "Add to playlist" offers only owned playlists. The probe confirmed the collection includes owned playlists. |
| 33 | Duplicate in playlist | `onDuplicates=FAIL`; show "Already in <playlist>". No "add anyway" in v1. |
| 34 | Library layout | One section with sub-tabs Playlists, Albums, Tracks, Artists; `tab`/`shift+tab` and clicks switch; each tab loads on first visit. |
| 35 | Filter reach | While a filter is active, the remaining pages load in the background (up to 5000 items per tab, notice at the cap); matches title, artist, and album, ignoring case and accents. |
| 36 | Sort | Newest added first; `o` cycles the server-side sort for the tab (date added, title, artist name where the tab has one) and reloads. |
| 40 | Discovery layout | Sub-tabs like the Library: Daily, Discovery, New Releases; `tab`/`shift+tab` and clicks switch; tabs with no mixes are left out. Search moves to the same layout in its own task. |
| 41 | Mix reload | Load when Discovery is first shown, and again on a visit more than an hour later. No refresh key. |
| 42 | Favorites on mixes | `f`/`F` work on mix rows like any playlist (the user confirmed TIDAL accepts mixes as favorites). |
| 43 | Radio and Similar keys | `R` radio on tracks and artists, `S` similar on tracks, artists, and albums, in every view; other rows get a notice saying what is supported. |
| 44 | Search hint lead | `/ search · tab next tab` lead the Search hint line; with the sidebar focused on Search, `/ search` comes first. |
| 45 | `tab` in the search input | `tab`/`shift+tab` leave the input and switch to the next / previous tab. |
| 46 | Artist view | Stays a grouped list in TASK-024; its own backlog task moves it to sub-tabs. |
| 37 | Rename migration | None. Run `tidal-tui -logout` before switching and move `config.toml` by hand; the README says nothing about it. |
| 38 | Rename timing | After v1 is feature-complete, as its own ticket. Until then the code, paths, and docs keep `tidal-tui`. |
| 39 | Affiliation notice | One README line under the intro, "needle is an independent project and is not affiliated with or endorsed by TIDAL.", also printed by `-version`. |
| 47 | Now-playing title | Bold on 2 rows (the second row blank); the real 2x title (OSC 66) is its own task, starting with a spike in kitty. |
| 48 | Enter on the current track | Same track ID as the current track, in any list, toggles pause. |
| 49 | Clearing the queue | `D` asks first ("Press D again to clear the queue"); a second `D` within 2 s clears the upcoming tracks. |
| 50 | TASK-014 scope | Login stays in the terminal before the TUI starts; logout stays the `-logout` flag. TASK-014 is the Account view only. |
| 52 | Highest quality | Remembered across runs in a file under `$XDG_STATE_HOME/tidal-tui/`, with the date first seen; `-logout` deletes it. |
| 53 | Account view lines | Username, country, email, highest quality, and the read-only `quality`, audio mode, and device settings with a pointer to `config.toml`. |

## Definition of Done

This is adapted from the CLAUDE.md template (Q14) and applies to every ticket. Check items off per ticket in backlog. Spike tickets are exempt from the code items.

- [ ] Every acceptance criterion in the ticket is met and has at least one test that fails without the change.
- [ ] `go test -race ./...`, `go vet ./...`, and `gofmt -l .` are clean.
- [ ] Changed non-UI packages are at 95%+ statement coverage. Changed TUI packages are at 80%+, using `teatest` or golden tests. API code has `httptest` coverage against a fake TIDAL API server, which is the e2e layer.
- [ ] Security self-review of the diff covers input validation, error messages that don't leak tokens or internals, concurrency on shared state, and no new fail-open paths. Findings go in the report.
- [ ] `go build ./cmd/tidal-tui` succeeds and the binary starts.
- [ ] The README is updated wherever behavior, configuration, keybindings, or tradeoffs changed.
- [ ] New dependencies are limited to those named in the ticket (for example the Charm v2 stack or a keyring library). No changes outside the ticket's scope.
- [ ] Logging uses `log/slog` to a file under `$XDG_STATE_HOME/tidal-tui/`, with `track_id` and `request_id` as structured fields where relevant. Logs never include tokens.


## New Items

1. Add a feature that if the audio volume is changed from the keyboard or the prev/pause/next buttons are used a small notification shows on the bottom of the viewport. This functionality exists in `cliamp` and I'd like to adopt it. If screenshots are needed, let me know.
2. For the playback scrubbing, we could use the left/right arrow keys to jump, 5/10/30 seconds depending on the frequency they are pressed. We can discuss at planning.
3. Add a post-MVP ticket to add an option to toggle a visualizer.
4. Post-MVP, Add EQ and presets
