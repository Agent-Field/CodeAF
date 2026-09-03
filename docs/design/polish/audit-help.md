# audit-help — the discovery, empty and refusal side of the surface

Audit lane. Read only; nothing here was changed. Frames are under
`docs/design/polish/frames/`, every one prefixed `help-`, captured from `bin/aforge`
on three homes: a **bare** home (only `.aforge/config.json`, no chats, no tasks, no
memories, no spend, no orders), a **fresh** home (no `.aforge` at all — first run),
and the seeded demo home for the populated readings.

Frame paths below are relative to `docs/design/polish/frames/` and each has a `.ans`
twin with the colour.

---

1. The search place's key hint is the router's default and says the opposite of the place's own body — `internal/tui3/place_search.go:354` (no `hint` method, so `internal/tui3/pages.go:286` answers with `placeHintWords`, `internal/tui3/pages.go:1227`) — the body says `enter opens the conversation at the matching turn.` and the line six rows under it says `enter talk about it`; both are on one frame, and the second one is false in both states — with results, `enter` opens the hit (`place_search.go:250`), with an empty box `placeTalk` returns nil and it does nothing at all. The manual agrees with the body and not with the hint (`internal/manual/chat/places.md:420`), so a developer who reads the foot learns a key that does not exist. — fix: give `placeSearch` a `hint` of its own beside the other five (`place_home.go:987`, `place_memory.go:847`, `place_settings.go:151`, `place_standing.go:916`, `place_tasks.go:1250`) — `enter opens it at that turn · ↑↓ pick · type to search · esc clears the words`; `placeTailed` adds `tab next place`. — sev: high — frames: help-empty-search.160x50.txt, help-empty-search.120x40.txt, help-empty-search.80x24.txt, help-search-query.160x50.txt, help-search-nohit.160x50.txt

2. The spend place inherits the same default hint and names none of its four real key classes — `internal/tui3/place_spend.go:466` (no `hint` method) — spend has `enter` on a row (`place_spend.go:579`), `→` with one verb `b the limits` (`place_spend.go:594`), a `▸ 14 more` fold, and a shift-arrow window (`place_spend.go:584`); the foot advertises `enter talk about it · alt+enter send it off as a task`, so the two keys a person standing on a row would actually press are the two that are not written down, and the one that is written down means something else. — fix: a `placeSpend.hint` — `enter opens what spent it · → the limits · shift+←→ move the days`; the window control already draws its own arrows on the head row, so the hint does not repeat them. — sev: high — frames: help-spend-demo.160x50.txt, help-empty-spend.160x50.txt

3. The default hint line does not fit an 80-column terminal and is cut mid-word, losing the way out — `internal/tui3/pages.go:1227` — `placeHintWords` is 92 cells; at 80 the frame draws `… · alt+. for the map · t…`, so on the commonest terminal size the two places that use it (search, spend) tell a developer nothing about `tab` or `esc`. The five places that write their own shorter sentence all fit. — fix: rows 1 and 2 remove both users of the default; failing that, give `placeHintWords` a narrow form the way `starterLine` already ladders (`welcome.go:519`) — drop `alt+. for the map` first, then `alt+enter …`. — sev: high — frames: help-empty-search.80x24.txt, help-empty-spend.80x24.txt

4. `/help` — the one sheet a person opens to learn the keys — names no way to reach any of the seven places — `internal/tui3/commands.go:859-941` — the hand-written key block lists thirty chords and not one of `alt+1`…`alt+7`, `alt+.` (the map), or `tab` in its place meaning; the manual promises `alt+1` "goes straight there from anywhere" (`internal/manual/chat/home.md:1573`), and the map is the surface's own chord list but is reachable only from inside a place you already had to know how to open. A developer who types `/help` from a cold start finishes it not knowing the places exist. — fix: three rows in `helpText`, spelled through `chords.say` like the `alt+enter` row above them — `alt+1…7` go to a place · the bar's own order, `alt+.` what else is here, `tab` on a place is the next place. — sev: high — frames: help-help-output.160x50.txt, help-help-output.80x24.txt

5. The search place has no slash-command door, and `/spend` opens something else — `internal/tui3/commands.go:63-398` — every other place has a typed door (`/home`, `/memory`, `/standing`, `/history`, `/settings`), search has none: `grep showPage(pageSearch)` finds nothing but the place's own file, so the only ways in are `alt+6`, `tab`, the bar, or typing `sea` on home — all of which have to be learned elsewhere first. Worse, `/spend` exists and is an alias of `/cost` (`commands.go:283`), which prints this conversation's bill rather than opening the spend place, so the one guess a developer makes lands somewhere else without saying so. — fix: add `{name: "search", desc: "everything said on this machine · alt+6"}` with a `case "search"` next to `case "home"` (`app.go:5664`), and add `· not the spend place, which is alt+5` to `/cost`'s note, or move `spend` off `/cost`'s alias list onto the place. — sev: high — frames: help-slash-palette.160x50.txt, help-help-output.160x50.txt

6. The search empty state reports what search is and never what to do — `internal/tui3/searchplace.go:245-251` — three declarative sentences, no verb, no example query, and the caret is at the foot of the frame in a box whose resting words are `say what you want done`, which is the errand prompt and not a search prompt. The tasks place, one `tab` away, shows the shape this one is missing: three sentences and then `no tasks yet — /task <brief> starts one` (`tasksplace.go:922`). — fix: a fourth `pal.dim` line in `searchTeach` in the asker's own words — `type words you remember — "the docker error", a person's name, a filename`; and the box's resting word on this place should be its own (`pages.go:1214` `placeRestWord` is shared by all seven and reads as an errand). — sev: high — frames: help-empty-search.160x50.txt, help-empty-search.80x24.txt

7. The manual listing wraps a long title into the name column and invents a page called `later` — `internal/manual/listing.go:50` — `Listing` builds `%-*s  %s` with no idea how wide the transcript is, so `keeping-an-eye`'s title runs past the frame and its last word lands on the next line flush at the page-name column. `/manual` and every `/manual <miss>` refusal both draw it, so the first list of pages a developer ever sees has a page on it that does not exist; typing `/manual later` then answers `there is no manual page named later`. — fix: cut the title to the room left after the name column, in `runManualCommand` where the width is known (`manualcmd.go:47`), or take a width in `Listing`. — sev: med — frames: help-manual-list.160x50.txt, help-manual-miss.160x50.txt

8. The command palette draws a fixed eight of fifty-two rows and never says the other forty-four exist — `internal/tui3/commands.go:566` — `menuRows = 8` is a constant, so on a 50-row terminal the list occupies eight rows under thirty-six blank ones; there is no `▸ 44 more` fold line, which every other list on this surface draws (`searchplace.go:137`, and the spend place's own `▸ 14 more`). `/manual` and `/help` are both below the fold, so `/` — the one door the greeting advertises — shows a developer /model through /compact and stops. The stated reason for the ceiling ("eight rows over the conversation is already half a short terminal") is true at 24 rows and not at 50. — fix: make the ceiling a function of the frame — `min(len(hits), max(menuRows, height/3))` — and append the fold line when it bites. — sev: med — frames: help-slash-palette.160x50.txt, help-slash-palette.80x24.txt

9. The memory place's empty state says what memory is and never what to do, and its hint promises shelf verbs on a page with no shelves — `internal/tui3/memoryplace.go:28-32` and `internal/tui3/place_memory.go:847` — the three sentences are all about the machine's behaviour; the closest thing to an act is a footer reading `nothing here is a setting, all of it is editable`, which names no key. Meanwhile `placeMemory.hint` falls through to `memoryShelfHint` whenever there is no shelf under the cursor, so the foot reads `enter open a shelf · type to filter · alt+s walk the shelves` over a body with nothing to open, nothing to filter and no shelves to walk. — fix: a fourth teaching line in the asker's verb — `nothing learned yet — say "remember that …" and the first line lands here`; and an arm at the head of `placeMemory.hint` for `a.mem.reading.teach` that returns that same sentence's keys instead of the shelf line. — sev: med — frames: help-empty-memory.160x50.txt, help-empty-memory.80x24.txt

10. The spend place's empty state never says that nothing has been spent, and names no next act — `internal/tui3/place_spend.go:501-503` — four sentences about what the ledger is, drawn over a machine whose ledger is empty; a developer cannot tell "this place has no rows yet" from "this place failed to read the ledger". The emptiness law is honoured — no `$0.00` anywhere in the body, and the top line correctly draws no allowance at all — so the only thing missing is the sentence. — fix: append one clause the way tasks does — `nothing spent yet — the first model call writes a line here`. — sev: med — frames: help-empty-spend.160x50.txt, help-empty-spend.80x24.txt

11. Memory's and search's teaching prose is truncated with an ellipsis rather than wrapped, so a sentence is unreadable on a narrow frame — `internal/tui3/memoryplace.go:307` and `internal/tui3/searchplace.go:116-119` — both call `fit(line, width)`; at 80 columns memory's second sentence draws `I put a line in here when it looked like it would matter later, and I only carr…` and its second half is simply gone. Tasks, standing and spend all go through `placeTeachProse` (`placebodies.go:120`), which wraps at a 76-cell reading measure and never cuts. — fix: route both through `placeTeachProse`/`placeTeachRows`, which is what those helpers exist for. — sev: med — frames: help-empty-memory.80x24.txt, help-empty-search.80x24.txt

12. Memory's and search's body rows sit one cell left of every other place's — `internal/tui3/place_search.go:407` and `internal/tui3/memoryplace.go:302` — `placeTeachRows` prepends `" "` to every row it builds (`placebodies.go:141`), so tasks, standing and spend hang from column 2 with the tab bar and the composer; search and memory build their rows themselves and start at column 1. Walking the bar left to right, the body steps sideways twice. — fix: the same reroute as row 11 for the teaching state, and `" " + text` in `placeSearch.body`'s row builder for the results. — sev: med — frames: help-empty-tasks.160x50.txt and help-empty-search.160x50.txt (compare line 5 of each), help-search-query.160x50.txt

13. `/help` spells one chord for macOS on every platform — `internal/tui3/steer.go:89` (`parkKey = "cmd+enter"`), drawn at `internal/tui3/commands.go:910` — the key sheet on a Linux box reads `cmd+enter    mid-answer: waits above the box`, naming a modifier that keyboard does not have; the actual keystroke arrives as `super+enter` or `meta+enter` (`steer.go:104`). Every other Mac-spelled chord on the sheet goes through `chords.say` (`commands.go:906`), which only ever substitutes `alt+`, so this one is unreachable by that door. The manual repeats the same spelling (`internal/manual/chat/keys.md:14`), so both accounts are wrong together on Linux. — fix: add the modifier to `chordSpelling` the way `alt+` is there (`chords.go:149`) and spell this row through it, so a Linux sheet says `super+enter` and a Mac sheet says `⌘enter`. — sev: med — frames: help-help-output.160x50.txt (line 25)

14. The map promises `→ verbs on this row` on places that have none — `internal/tui3/pages.go:1240` — `placeMapWords` is one fixed sentence drawn over every place, but `placeSearch` declares no `verbs` (falls to `placeBase.verbs`, `pages.go:264`), so on search the `→` it names opens nothing and falls through to the caret inside the box. SCREEN 3a's clause is that no key does anything that is not drawn; this is the inverse and just as costly — a key drawn that does nothing. — fix: build the map line from what the standing place actually declares, dropping the `→` clause where `pl.verbs(a)` is empty. — sev: med — frames: help-map-on-search.160x50.txt

15. The first-run block and the greeting that replaces it are centred by two different rules, so the wordmark jumps when setup ends — `internal/tui3/firstrun.go:753` (`top := (height - len(body)) / 2`, and `inner := min(width-4, setupWidth)` with `setupWidth = 64` at `firstrun.go:777`) against `internal/tui3/view.go:485` (`welcomeAbove` = two fifths of the slack) and `welcome.go:495` (`welcomeUnitWidth = 76`) — at 160x50 the setup wordmark stands at row 18 column 49 and the greeting's identical wordmark, one keypress later, at row 17 column 43. The greeting's two-fifths lift is written down and argued for; the setup screen's exact half is not, and the two are the only two screens on which this wordmark is ever drawn. — fix: give the setup block the greeting's own placement — `welcomeUnitWidth` for the measure and `welcomeAbove` for the lift — so the letterform does not move between the first screen and the second. — sev: med — frames: help-firstrun.160x50.txt, help-empty-home.160x50.txt, help-firstrun.120x40.txt, help-firstrun.80x24.txt, help-firstrun.60x30.txt

16. The first-run screen's refusal line is whatever Go error the writer produced, printed raw — `internal/tui3/firstrun.go:407, 429, 451, 468, 515, 525, 564` — seven of the setup's refusals are `err.Error()`, so the first sentence a new person can be shown on their first screen is a wrapped error chain with no cause a person can act on. The same file already holds the right shape three lines away — `not the shape of an openrouter key — they start with sk-or-` (`firstrun.go:574`) and `openrouter returned no usable key` (`firstrun.go:456`) — so this is inconsistency inside one screen rather than a missing idea. — fix: wrap each one the way the surrounding constants are written — cause, then what to do — keeping the Go text only behind `/debug`. — sev: med — frames: help-firstrun-bad.120x40.txt (the good shape, for contrast)

17. The wordmark's last glyph has no right stroke and reads as a clipped letter — `internal/tui3/welcome.go:414` — `'f': {"┌─ ", "├─ ", "│  "}` next to five closed glyphs draws `┌─┐ ┌─┐ ┌─┐ ┌─┐ ┌─┐ ┌─`, which at every width looks like a word the terminal cut off. It is not clipped — the letterform is genuinely open — but it is the first thing on the first screen and it reads as a bug. Reported here because the wave's brief named it as a truncation at 120 columns; the frames say it is identical at 60, 80, 120 and 160, so no width change fixes it. — fix: close the letter — a right terminal on the crossbar row (`├─┤`-style) or a descender stroke — or accept it and note it, but do not chase it as a layout bug. — sev: low — frames: help-firstrun.60x30.txt, help-firstrun.80x24.txt, help-firstrun.120x40.txt, help-firstrun.160x50.txt

18. A search that finds nothing reports the emptiness and nothing else — `internal/tui3/searchplace.go:119` — one dim line, `nothing on this machine says "zzqqxx"`, alone in a forty-three row body. It is honest and it is the whole answer; a developer cannot tell a typo from a machine that has not indexed anything, and there is no next act. — fix: a second clause on the same line or under it — `· try fewer words, or a name` — and, where `a.searchStore == nil` (`place_search.go:150`), say so rather than falling back to the teaching text, which currently makes "no index behind this" and "an empty box" look identical. — sev: low — frames: help-search-nohit.160x50.txt

19. Three more places' hint lines promise row verbs over a body that has no rows — `internal/tui3/place_tasks.go:1250`, `internal/tui3/place_standing.go:916`, `internal/tui3/place_memory.go:847` — on the bare home the tasks foot says `type to filter` with nothing to filter, standing says `enter open where it was asked` with nothing to open, memory says `enter open a shelf · alt+s walk the shelves` with no shelves. Each is smaller than rows 1 and 2 because the body above it does say what to do; the cost is that the foot of a teaching page names keys that do nothing. — fix: one arm at the head of each `hint` for the teaching state, returning the keys that are true there — on all three that is `tab next place · esc`. — sev: low — frames: help-empty-tasks.160x50.txt, help-empty-standing.160x50.txt, help-empty-memory.160x50.txt

20. The entry note dumps the absolute transcript path over four wrapped lines — `internal/tui3/welcome.go:350` and `internal/tui3/app.go:2327` (`a.note("resumed " + a.hostedPath(a.file))`; `hostedPath` at `host.go:315` prefixes a host name and otherwise returns the path untouched) — on an 80-column terminal the first thing on screen when a conversation is reopened is four lines of `.aforge/v3/projects/<slugged-absolute-path>/<hex>/transcript.jsonl`, and at 60 columns it is seven of the thirty rows. `/help`'s closing `session · <path>` row does the same (`commands.go:939`). The conversation has a name; the path is machinery. — fix: say the conversation's name and tilde the home — `resumed <title>` with the path behind `/status`, which already prints one fact per line. — sev: low — frames: help-empty-home.80x24.txt, help-empty-home.60x30.txt, help-help-output.160x50.txt

21. `ctrl+r` appears twice on the key sheet with two different meanings — `internal/tui3/commands.go:901` (`spell it out · what the draft means`) and `internal/tui3/commands.go:937` (`ctrl+r ctrl+y  in /files: reveal the folder it is in`) — thirty-six rows apart, with nothing on either row saying the other exists. The second is scoped to `/files` and the first is not, so they do not collide in the code; on the sheet they read as a contradiction. — fix: name the scope on the first row too, or move the `/files` pair next to it. — sev: low — frames: help-help-output.160x50.txt (lines 20 and 41)

22. The unknown-command refusal is written in machinery register — `internal/tui3/app.go:5887` — `unknown command: /nosuchthing · try /help` leads with a compiler's noun and a colon; every other refusal on this surface is a sentence about the world (`there is no manual page named xyzzy`, `that conversation is not on this machine any more`). It does say what to do, which is the important half. — fix: `there is no command called /nosuchthing · / lists them`, matching `manualcmd.go:53`'s wording, and follow it with the list the way `/manual` does rather than pointing at a second command. — sev: low — frames: help-badcommand.160x50.txt

23. `?` is bound to nothing and is advertised nowhere; help is reachable only by typing a slash command — `internal/tui3/commands.go:397` (`?` is an alias of `/help`, i.e. `/?`, not a key) — pressing `?` on the conversation or on any place types a literal `?` into the composer. The greeting's three clauses name `/ shows commands` and nothing else (`welcome.go:512-515`), and `/help` and `/manual` are both below the palette's eight-row fold (row 8), so from a cold start the shortest honest route to the key sheet is typing six characters you have to already know. The manual claims no bare `?` key, so nothing is lying — this is a gap, not a disagreement. — fix: either bind `?` over an empty composer to `/help` and add it to the greeting's ladder, or add `? for help` as a fourth clause on `starterLine` after `/ shows commands` — the ladder already drops clauses on a narrow frame, so it costs nothing at 60 columns. — sev: low — frames: help-question-key.160x50.txt, help-empty-home.160x50.txt, help-slash-palette.160x50.txt

---

## Checked and found right — recorded so no one re-audits them

- **The emptiness law holds on every empty place.** On the bare home the top line draws no
  allowance at all (`help-empty-tasks.160x50.txt` line 1 is the clock alone), the spend body
  draws no `$0.00` and no `0 tok`, and the greeting's status row is
  `aforge · <model>   crew balanced · idle` with no money and no token count — exactly what
  `internal/manual/chat/empty-screen.md:29` says it should be. The one `$0.00` that appears
  is on the live status line of a conversation, which is the stated exception.
- **No `lipgloss.Color("…")` literals anywhere in `internal/tui3`** — `grep` over the whole
  package (tests excluded) returns zero. Every colour goes through the palette.
- **No banned machinery vocabulary** — `auditor`, `verdict`, `verified`, `refuted` appear in
  no person-facing string on these surfaces.
- **Every place opens on a machine with nothing on it.** `alt+1` … `alt+7` all raise a page
  on the bare home; none refuses, none draws a blank body. `pages.go:261`'s law holds.
- **The masked key box reveals only the last four characters** (`firstrun.go:888`) — standard,
  deliberate, not a defect.
- **`openaf` is the product's own wordmark on purpose**, not a stale name — the manual states
  it (`starting-aforge.md:22`, `commands.md:234`) and `styles.go:1648` is the one source.
- **`tab` in a conversation means "the last conversation" and on a place means "the next
  place"**, and `/help`'s line for it is correct for where it is read — the manual says the
  same thing twice (`keys.md:493`, `keys.md:1273`). Not a disagreement.
- **The map draws the digits onto the tab bar** when `alt+.` is pressed
  (`help-map-on-search.160x50.txt` line 2), which is the one place on a place the numbers are
  taught. Its only fault is row 14.

## What was captured

Bare home (config only, nothing in it): home, tasks, standing, memory, spend, search,
settings at 160x50; home at 120x40, 80x24, 60x30; search at 120x40 and 80x24; memory and
spend at 80x24. First run (no `.aforge` at all): 160x50, 120x40, 80x24, 60x30, plus the
bad-key refusal and the screen after `esc`. Help doors: `?`, `/`, `/ma`, `/help`, `/manual`,
`/manual <page>`, `/manual <miss>`, `/manual <question>`, `/nosuchthing`, and the map over
search. Demo home: search resting, with words, with no hit, and the spend place with a
ledger behind it.

---

## fixed

Fix lane, on `ui/polish-v0`. Frame paths are relative to `docs/design/polish/frames/`;
every `-after` frame has a `.ans` twin and the `-before` reading is the frame the row
above already names.

**Row 1 — the search place's foot is its own** · `internal/tui3/pages.go` (the `hint`
default deleted from `placeBase`, so a place without one no longer compiles),
`internal/tui3/place_search.go` (`placeSearch.hint`, in two states: `searchHitHint` over a
result and `searchAskHint` where there is no row to stand on) · tests
`TestEveryPlaceSaysItsOwnKeys`, `TestTheSearchFootNamesTheKeyThatOpensAHit` · before
`help-empty-search.{160x50,120x40,80x24}.txt` → after
`help-empty-search-after.{160x50,120x40,80x24,60x30}.txt`

**Row 2 — the spend place's foot is its own** · `internal/tui3/place_spend.go`
(`placeSpend.hint`, built from the row under the cursor and from whether the window
control is drawn at this width) · test `TestTheSpendFootNamesTheKeysAPersonWouldPress` ·
before `help-empty-spend.{160x50,80x24}.txt` → after
`help-empty-spend-after.{160x50,120x40,80x24,60x30}.txt`

**Row 3 — hints drop whole hints** · `internal/tui3/pages.go` (`hintFit`, and both draw
sites routed through it instead of `fit`) · the rank: everything from `tab next place`
onward is protected, and what is dropped is taken from the clause nearest that tail
working backwards, so the composer's own line loses `alt+. for the map` first and
`enter talk about it` last · test `TestAHintDropsWholeClausesAndKeepsTheWayOut` · before
`help-empty-search.80x24.txt` (`… · alt+. for the map · t…`) → after
`help-empty-search-after.80x24.txt`

**Row 4 — /help names the way into the places** · `internal/tui3/commands.go` (three rows
under the `tab` row, spelled through `chords.say`), `internal/tui3/pages.go`
(`placeWordList`, read off `placeOrder` so the sheet cannot drift from the bar) ·
`internal/manual/chat/commands.md` · test `TestTheKeySheetNamesTheWayToEveryPlace` ·
before `help-help-output.{160x50,80x24}.txt` → after
`help-help-output-after.{160x50,80x24}.txt`

**Row 5 — /search exists and /spend opens the spend place** · `internal/tui3/commands.go`
(two rows added; `spend` taken off `/cost`'s alias list and `/cost`'s own row now says
which question it answers), `internal/tui3/app.go` (`case "search"` and `case "spend"`
beside `case "home"`), `internal/manual/chat/commands.md`, `internal/manual/chat/places.md`,
`internal/manual/chat/models-and-cost.md`; `internal/tui3/statusnote_test.go`'s alias table
updated, since it pinned the behaviour this row changes · test
`TestSearchAndSpendHaveTypedDoorsOfTheirOwn` · after `help-search-door-after.160x50.txt`,
`help-spend-door-after.160x50.txt`

**Row 6, 9, 10 — an empty place says what to do next, in a verb** ·
`internal/tui3/searchplace.go` (`searchExampleWord`, a fourth teaching line),
`internal/tui3/place_spend.go` (`spendTeachEmptyWord` on the end of `spendTeach`),
`internal/tui3/memoryplace.go` (`memoryEmptyWord`, drawn only where the page is bare —
`memoryReading.bare`), `internal/tui3/place_memory.go` (`memoryBareHint`: a page with no
shelves promises no shelf keys) · `internal/manual/chat/places.md` · test
`TestAnEmptyPlaceSaysWhatToDoNext` · before `help-empty-search.160x50.txt`,
`help-empty-spend.160x50.txt`, `help-empty-memory.160x50.txt` → after
`help-empty-search-after.160x50.txt`, `help-empty-spend-after.160x50.txt`,
`help-empty-memory-after.160x50.txt`

**Row 8 — the palette fills the frame and says what is hidden** ·
`internal/tui3/commands.go` (`menuRows` is a floor rather than a ceiling; `menu.height`
takes the room as a number, `menu.fit` counts rows and lines, `menu.rows` reserves the
fold line before laying rows into what is left), `internal/tui3/palette.go` (the room is
measured once, before any list is asked what it wants, and handed in) ·
`internal/manual/chat/commands.md` · test
`TestThePaletteFillsTheFrameAndSaysWhatIsHidden` · before
`help-slash-palette.{160x50,80x24}.txt` (8 rows of 52, no fold) → after
`help-slash-palette-after.{160x50,120x40,80x24,60x30}.txt` (44 rows and `▸ 9 more` at
160x50)

**Row 15 — the wordmark does not move when setup ends** · `internal/tui3/firstrun.go`
(`welcomeUnitWidth` for the measure and `welcomeAbove` for the lift; `setupWidth` deleted
with the second rule it was the only user of) · test
`TestTheWordmarkDoesNotMoveWhenSetupEnds`, which asserts the column at 160x50, 120x40,
80x24 and 60x30 · before `help-firstrun.{160x50,120x40,80x24,60x30}.txt` (column 49 at
160x50) → after `help-firstrun-after.{160x50,120x40,80x24,60x30}.txt` (column 43, which is
the greeting's)

**Rows 11 and 12, memory's half — the teaching wraps, and it hangs from the body's own
column.** Memory's three sentences went through `fit`, so at 80 columns the second one drew
`…and I only carr…` and its other half was simply gone; and they started at column 1 where
tasks, standing and spend all start at column 2. The prose is WRAPPED INTO THE READING now
(`memoryReading.wrapped`, at the same `teachMeasure` every other place's teaching uses) rather
than cut on the way out, so one line of the reading is still one drawn row — which is the law
the cursor, the hit map and the scroll are built on — and `memoryPlace.remeasure` re-lays it
when the frame changes, keeping the cursor on the thing it was standing on. Every prose row
and the footer take the body's one-cell lead.
Files: `internal/tui3/memoryplace.go`, `internal/tui3/place_memory.go`.
Test: `TestTheMemoryTeachingWrapsAndHangsFromTheBodysColumn`
(`internal/tui3/spendmemwords_test.go`) — at 60, 80, 120 and 160 no row is over the frame, no
row ends in an ellipsis, every row hangs from column 2, and the longest sentence survives
whole across the lines it took.
Frames: `frames/spendmem-memory-empty-after.{160x50,120x40,80x24,60x30}.txt` against
`spendmem-memory-empty.*.txt`.
**Search's half of both rows is untouched** — `searchplace.go` and `place_search.go` are not
this lane's files.

**Row 2's foot, checked rather than changed.** `placeSpend.hint` already names
`enter opens what spent it · → the limits · shift+←→ move the days`, and after this lane's
row seventeen of `audit-settings.md` the `enter` it promises is true on the loudest-day row as well as on the rows under
*what it was for*.

### Not this lane

Rows 7, 13, 14, 16, 17, 18, 19, 20, 21, 22 and 23 are untouched; 11 and 12 are half done
(memory's half is above, search's is not this lane's). Two of them are worth a note for whoever takes them:

- **Row nineteen** is now half done. Memory's teaching-state foot is fixed here (row 9's other
  half); tasks' and standing's are not, and both live in files this lane was told not to
  edit.
- **Row 3's fitter is general.** `hintFit` is the one door every foot on a place goes
  through, so a row that adds a clause to any place's hint gets the ladder for free —
  and a clause that must never be dropped goes after `tab next place`, not before it.

---

## fixed — the second pass (`?`, the refusals, the sheet's own keys)

Same lane, later wave, on `ui/polish-v0`. Frames are prefixed `help2-`; the
`-before` reading of each is the frame the row it closes already names.

**Row 23 — `?` opens the keys, and never eats a typed one** · `internal/tui3/commands.go`
(`helpAskKey`, `app.helpAsk`, `helpAskWord` and the sheet's new first key row),
`internal/tui3/input.go` (one rung, beside the offer letter's),
`internal/tui3/placekeys.go` (`?` normalised to `placeMapKey`, so it toggles and dismisses
identically to the chord), `internal/tui3/app.go` (`landingKeysWord`) ·
`internal/manual/chat/{keys,commands,screen,empty-screen}.md`

**What it opens, and why.** `?` means one sentence — *show me the keys for where I am
standing* — and this surface has two screens, so it has two renderings of one gesture, the
way `helpText` is one table with two:

- in a **conversation** it runs `/help`, the whole key sheet, into the transcript where it
  can be scrolled and searched;
- on a **place** it draws **the map** (`alt+.`) — that place's own keys, in the cells the
  foot was already using. Printing the sheet from a place would mean leaving the room to
  answer a question about it, and since row 14 below the map now names only the keys that
  place really has.

**The guard is structural.** The box must be EMPTY — the offer key's rule word for word
(`keys.go`) — so a `?` typed into a sentence, a filter, a picker or a panel is a question
mark and nothing else. Every overlay on this surface is read above that rung.
It is advertised on the first key row of `/help` and on the line every session opens with:
`esc interrupts · ctrl+c twice quits · ? for help`. It is **not** on the greeting's starter
ladder: those three clauses measure 75 cells inside a 76-cell unit
(`welcomeUnitWidth`), which the first-run block now shares, so a fourth clause would
either never draw or move the setup screen.
Tests `TestTheQuestionMarkOpensHelpAndNeverEatsATypedOne`,
`TestTheQuestionMarkIsAdvertisedOnTheSheetAndTheOpeningLine` ·
after `help2-qmark-chat-after.120x40.txt` (the sheet), `help2-qmark-after.{160x50,120x40,60x30}.txt`
(the map, on home), `help2-manual-miss-after.160x50.txt` (the opening line)

**Row 16 — the first screen refuses in sentences** · `internal/tui3/firstrun.go`
(`setupConnectFailedWord`, `setupBrowserWord`, `setupSignInLostWord`,
`setupSaveFailedWord`, and `setupSaid` behind the four `row.Apply` sites) ·
`internal/manual/chat/getting-started.md`. The three browser-trip refusals are authored
outright — nothing a listener, an `xdg-open` or a closed tab returns is a sentence for a
person. The four that write through a **settings row** keep the REGISTRY's own words and
replace only the operating system's, and the test for which is which is structural rather
than a guess at the prose: every refusal the registry authors is a bare `fmt.Errorf`
(`that's not a dollar amount — a number, or none for no limit`, `pick one of: frugal,
balanced, max`, `OpenRouter key is set by OPENROUTER_API_KEY`), while every disk failure is
wrapped around the operating system's own error with `%w` — so an error that wraps another
error is the machine talking. That is the settings panel's own bargain kept on this screen
(`app.applySetting` draws `row.Apply`'s refusal as it stands).
Tests `TestTheSetupRefusesInSentencesAndNeverInGoErrors` (which makes a profile directory
read-only and asserts the drawn line carries no `/`, no `:`, no `config` and no `denied`),
`TestTheSetupKeepsASettingsOwnRefusalAboutWhatWasTyped`

**Rows 13 and 21 — the key sheet's own two wrong keys** · `internal/tui3/chords.go`
(`chordCmdWord`, `chordCmdGlyph`, `chordSuperWord`, `chordSpelling.cmdWord`, and `say`
substituting both modifiers), `internal/tui3/commands.go` (the park row spelled through
`chords.say`; the spell-it-out row scoped) · `internal/manual/chat/keys.md`. `cmd+enter`
stays the authored spelling and is drawn `⌘enter` on a Mac and `super+enter` everywhere
else — one door, the way `alt+`/`⌥` already went. And the two `ctrl+r` rows now each say
where they act: `over a draft:` and `in /files:`. They never collided in the code; they
collided on the page, which is the one page a person reads to learn the keys.
Test `TestTheKeySheetSpellsTheSendChordForThisKeyboardAndScopesBothCtrlR` ·
before `help-help-output.160x50.txt` (lines 20, 25 and 41) → after
`help2-help-output-after.{160x50,80x24}.txt`, `help2-qmark-chat-after.120x40.txt`

**Row 14 — the map names row verbs only where the row has them** · `internal/tui3/pages.go`
(`placeMapVerbWords` named out of `placeMapWords`, and `app.placeMapSaid` asking the
standing place what it declares) · `internal/manual/chat/places.md` · test
`TestTheMapNamesTheRowVerbsOnlyWhereTheRowHasThem` · before `help-map-on-search.160x50.txt`
→ after `help2-map-on-search-after.160x50.txt` (search: no `→` clause) against
`help2-qmark-after.160x50.txt` (home: it keeps it)

**Rows 11, 12 and 18 — search's half** · `internal/tui3/searchplace.go` (the teaching goes
through `placeTeachProse` a sentence at a time; `searchHung` puts the body's one-cell lead
on every row and the rows are built into the cell that leaves; `searchNothingSaid`;
`searchNoIndexWord` and `searchReading.noIndex`), `internal/tui3/place_search.go`
(`rebuildSearch` reads whether there is an index at all) · `internal/manual/chat/places.md`
· `internal/tui3/searchplace_test.go`'s cursor test updated, since it pinned the column
this row moves. A search that finds nothing now says `· try fewer words, or a name`, and a
window with no store behind it says so rather than reporting an empty result — "nobody said
that" and "nothing looked" were the same line.
Tests `TestTheSearchTeachingWrapsAndHangsFromTheBodysColumn`,
`TestASearchThatFindsNothingSaysWhatToDoAndAMissingIndexSaysSo` · before
`help-empty-search.{80x24,160x50}.txt`, `help-search-nohit.160x50.txt` → after
`help2-empty-search-after.{160x50,120x40,80x24,60x30}.txt`,
`help2-search-nohit-after.160x50.txt`

**Row 7 — the manual listing never invents a page** · `internal/tui3/manualcmd.go` (all
three listings go through `app.noteBlock`). The listing is one line per page and an
ordinary note RE-FLOWS its text, so a long title wrapped and its tail landed at the column
the page NAMES are in — `/manual` drew a page called `later`, and `/manual later` then
answered that there is no such page. `noteBlock` is the door for exactly this shape: a note
whose line structure is its meaning, cut rather than re-flowed. No new formatter, and
`manual.Corpus.Listing` stays the one place a listing is built.
Test `TestTheManualListingNeverInventsAPage` · before `help-manual-list.160x50.txt`,
`help-manual-miss.160x50.txt` → after `help2-manual-list-after.{160x50,80x24}.txt`,
`help2-manual-miss-after.{160x50,80x24}.txt`

**Row 20 — the entry note, on the door that still dumped the path** ·
`internal/tui3/welcome.go` (`app.openSession` says `app.resumedNote()`),
`internal/tui3/app.go` (`/help`'s `session · …` row written against `$HOME`). The launch
line was fixed a wave ago and the PICKER's road was not, so opening a conversation from the
greeting still put four to six wrapped rows of absolute transcript above the person's first
message while opening the same conversation from the launch line said its name.
Test `TestOpeningAConversationFromThePickerNamesItAndNotItsPath` · after
`help2-manual-miss-after.160x50.txt` (line 2: `resumed · wrapping`)

**Row 22 — the unknown command is a sentence** · `internal/tui3/commands.go`
(`unknownCommandWord`, `unknownCommandLead`, `unknownCommandDoorWord`),
`internal/tui3/app.go` (the one call site) · `internal/manual/chat/{commands,attaching-files}.md`
· `dropkeys_test.go`, `homedrop_test.go`, `homeslash_test.go`, `payload_test.go`,
`surface_test.go`, `tui3_test.go` all updated: **eight** of those assertions are negative —
"this must NOT say unknown command" — and every one of them would have gone on passing
against the new wording, which is the defect this lane was told to watch for. They read
`unknownCommandLead` now. The refusal points at `/` — one keystroke, the list itself —
rather than at a second command to type. Test
`TestAnUnknownCommandIsRefusedInASentenceAndPointsAtTheList` · before
`help-badcommand.160x50.txt` → after `help2-badcommand-after.160x50.txt`

**Row 10, checked rather than changed.** The spend place's empty state was closed by the
sibling lane above (`spendTeachEmptyWord` — `nothing spent yet — the first model call
writes a line here.`). Nothing to do.

### Files outside this lane's list, and why

`internal/tui3/input.go` and `internal/tui3/placekeys.go` each took ONE rung so that `?`
could be bound at all — a key cannot be bound from the file that describes it — and
`internal/tui3/app.go` took three one-line changes (the opening line, `/help`'s path, the
unknown-command call site). None of the three is a file another lane was named as holding.
