# audit — the home place, the launcher and the first run

Audit only; nothing in this pass edits source. Every row below was seen on a frame
captured from `bin/aforge` in a real terminal against the demo home, and traced to the
line that decides it. Frames live in `docs/design/polish/frames/` and are named
`home-*`.

Two laws this surface already keeps, said out loud so nobody spends a wave re-finding
them: **there is not one `lipgloss.Color("…")` literal** in `home*.go`,
`place_home.go`, `homeband_*.go`, `firstrun.go`, `homebridge.go`, `switcher.go`,
`pulse.go` or `pages.go` — every ink goes through the theme role map. And the
**emptiness law holds**: no `$0.00` and no `0 tok` reaches a person; the strings only
appear inside comments and tests that pin their absence. The banned machinery words
(`auditor`, `verdict`, `verified`, `refuted`) reach nobody either —
`taskUnverifiedWord` is an identifier whose value is `"needs your look"`.

---

1. The first screen of a fresh install brands the product `openaf`, while every screen after it says `aforge` — /home/santosh/af-polish/internal/tui3/styles.go:1648 (`const product = "openaf"`) against /home/santosh/af-polish/internal/tui3/pulse.go:72 (`pulseName = "aforge"`) — the wordmark a new person meets spells a different product from the binary they typed, the repo, the docs and row 0 of every place they will see thirty seconds later; the clash is visible **within one screen**, because the prose three rows under the wordmark reads "aforge stores it on this machine" (`setupConnectWord`), and the same constant also writes the OAuth consent line "openaf wants to connect your … account" (connect.go:438) and the desktop notification title (notify.go:52) — fix shape: `product` is already the one-source constant its own doc comment claims to be, so change its value to `"aforge"` and add `f` to `wordmarkGlyphs` — it is there — plus `r` and `g`, which are not; if the letterforms for `r`/`g` are the blocker, draw the wordmark from `pulseName` in the ASCII branch and retire the second spelling rather than keeping two — sev: high — frames: `home-firstrun.120x40.txt`, `home-firstrun.80x24.txt`, `home-firstrun.60x30.txt`, `home-idle.120x40.txt` (line 1)

2. **H1.** The list stops at eight rows whatever the terminal's height, so a 40-row window draws 14 rows of content and 22 of nothing while the list is still folded — /home/santosh/af-polish/internal/tui3/switcher.go:31 (`const switcherShown = 8`), spent at switcher.go:508 and switcher.go:581, capped through switcher.go:258 (`cap`) — the height never enters the decision: the reading is built in `buildSwitch` (place_home.go:114) from a world and a view struct with no room in scope, and `room` first appears one layer later at place_home.go:884 (`placeHome.body`), by which time the fold has already been drawn; a developer on a full-height window pays a keystroke (`enter` on the fold, or `alt+q`) to see rows that had 22 empty lines waiting for them, and the frame simultaneously asserts "12 chats" and hides five of them under a blank half-screen — fix shape: `switcherShown` becomes a floor rather than the cap — thread the body's `room` into the reading (either build it in `placeHome.body`, which is pure and cheap, or re-cap at draw) and make `cap(n)` return `max(switcherShown, roomLeftAfterTheHeadings)`, keeping the fold line only when rows genuinely do not fit; note the constant carries no doc comment at all, which in this file is itself the tell — sev: high — frames: `home-idle.120x40.txt`, `home-idle.140x45.txt` (28 blank rows), `home-idle.160x50.txt`, `home-grouped.120x40.txt`

3. **H2.** The right-hand card needs 160 columns and is absent at every ordinary window — /home/santosh/af-polish/internal/tui3/homebridge.go:83 (`homeCardMin = homeSwitchFull + homeGutter + homeCardCol`), from homebridge.go:72 (`homeSwitchFull = 120`) and homebridge.go:66 (`homeCardCol = 36`), applied at home.go:3985 — the arithmetic is honest but the input is not: `homeSwitchFull` is documented as "the width at which a row can carry its mark, its name, its project tag, its note and its age with none of them giving way", and the only row that actually wants 120 cells is the one carrying a long `asks: …` note, which is *the fact the card exists to carry properly*; so the layout reserves the list's width for a clause it would happily hand to the card, and a 120- or 140-column MacBook window gets neither — a developer on a normal window never sees the repo state, the spend, the token count or the verb list for the row they are on, and cannot discover the card exists — fix shape: measure `homeSwitchFull` against a row **without** the ask note (~96 cells for name + project + age at the demo's widths), which puts `homeCardMin` near 136, and let the note give way to the card past that width — `switcherPaintRow`'s drop ladder (switcher.go:770) already drops the note first and is the mechanism for it — sev: high — frames: card present in `home-idle.160x50.txt` and `home-idle.200x50.txt`; absent in `home-idle.140x45.txt` and `home-idle.120x40.txt`

4. A long note on a list row clips the conversation's **name** to eight cells before a single fact is dropped — /home/santosh/af-polish/internal/tui3/switcher.go:770 (`for switcherTailWidth(parts)+ansi.StringWidth(glyph)+2+8 > width`) — the loop's guard is an eight-cell *floor on the name*, not "the name is whole", so the ladder never fires while the title has eight cells left: at 100 columns the ask row draws `? tell me when CI goe… aforge-v2 asks: May I re-run the typecheck job to see whether it is flaky? 4m`, spending 78 cells on a sentence and 18 on the identity — this is rowfit.go law 1 inverted on the list this surface exists to choose from, and the same file states the correct test twenty lines below (switcher.go:808, the door-word growth, which asks that `row.title` fit **whole**) — fix shape: replace the `+8` in the loop guard with `ansi.StringWidth(row.title)` so facts drop until the name fits, falling back to the eight-cell floor only when every fact has already gone; better still, route the row through `rowPlan.fit`, which is exactly this law written down — sev: high — frames: `home-idle.100x30.txt` (line 6), `home-idle.120x40.txt` (line 6, fits), `home-idle.80x24.txt` (line 6, note dropped correctly)

5. **H3a.** The card's place line is cut from the left, so the project identity is the first thing spent — /home/santosh/af-polish/internal/tui3/place_home.go:605 (`fitLeft(strings.Join(parts, " · "), width)`), with `fitLeft` at /home/santosh/af-polish/internal/tui3/homebands.go:167 — `fitLeft` is right about a *path alone* (the basename distinguishes it, and its doc comment says exactly that), and wrong the moment the path is joined to facts about it: what survives is the tail of the whole joined string, so the card draws `…e-v2 · master, 3 files dirty · here` at 160 and `…rge-v2 · master, 3 files dirty · open in another window` at 200 — the card's title one row above already names the conversation, and the place line's only job is saying **which checkout**, which is the half that goes — fix shape: fit the parts as a ranked tail, not as one string: `fitLeft` the address alone to whatever it needs, then add `master, 3 files dirty` and then `here` through `rowTail`, so the lower-value trailing facts drop whole and the address keeps its cells — sev: high — frames: `home-idle.160x50.txt` (line 6), `home-idle.200x50.txt` (line 6), `home-short.160x24.txt` (line 6, worse: `…iles dirty · open in another window`)

6. **H3b.** Card rows clip the label to keep the tail, so a deliverable's name is spent on a price — /home/santosh/af-polish/internal/tui3/homebands.go:147 (`if label != "" && labelRoom >= floor { shown := fit(label, labelRoom) }`), reached from place_home.go:686 (`bandSides(ctx.width-2, 0, 8, label, cost, …)`) and homeband_deliverables.go's `bandSidesWithSeparator` — the tail is measured first and given every cell it wants, and the label takes what is left with an ellipsis, down to a floor of 8; the card draws `✓ Port the picker onto the ne… $0.31`, which names no piece of work and prices it precisely — `bandSides` is a **second row fitter with the opposite law from rowfit.go**, and it is what four call sites in `homeband_work.go`, `homeband_projectsessions.go`, `homebands.go` and `place_home.go` draw through — fix shape: `bandSides` adopts `rowPlan.fit`'s rule — a label that had to be cut takes the whole row and the tail goes to the second line (the function already has that two-line branch at homebands.go:152, it is just gated the wrong way round: it fires when the *label* is short, not when the label had to be cut) — sev: high — frames: `home-short.160x24.txt` (line 12), against `home-idle.200x50.txt` (line 12, whole at 56 cells)

7. First run on a short terminal loses its input box, its keys and its way out — /home/santosh/af-polish/internal/tui3/firstrun.go:768 (`if len(lines) > height { lines = lines[:height] }`) — the setup block is centred and then clipped from the **bottom**, so at 12 rows a person sees the wordmark, the question and four lines of prose, and nothing else: no `›` box, no `enter connects in browser · paste a key · esc not now`, and therefore no visible key that leaves the screen; the pulse, the tab bar and the composer are not drawn on this frame either, so there is no other furniture to fall back on — a developer on a split pane meets an install that appears to have hung — fix shape: the block gives up rows from the middle, never the foot: the prose (`setupConnectWord`) is the droppable part and the box plus the hint line are the part that must survive, exactly as `homeBands`/`homeCardStack` already drop bands from the bottom while keeping the title — sev: high — frames: `home-firstrun-tiny.120x12.txt`, against `home-firstrun-short.120x16.txt` (whole at 16)

8. `esc not now` ends first run for good, and two of its three questions are never asked again — /home/santosh/af-polish/internal/tui3/firstrun.go:220 (`endSetup` writes `config.MarkSetupSeen` whichever way it ended) against firstrun.go:167 (`firstRun := … && config.SetupSeenAt(dir).IsZero()`) — pressing esc on **step 1 of 3** stamps `setup_seen_at` (verified: the config the rig wrote holds nothing but that key), and on every later launch only the `providerMissing` branch reopens the flow, with `setupKey` alone — so the crew question and the daily-budget question are silently retired by a key labelled "not now", and the returning screen says a bare `setting up` (firstrun.go:787) instead of `1 of 3`, giving no sign that the flow shrank; the file's own comment at firstrun.go:206 states the behaviour plainly — "the first-run questions are gone for good" — so the code is deliberate and the **label is the defect** — fix shape: either spell the key what it does (`esc skip setup`), or make esc skip the step on screen and leave `setup_seen_at` unwritten until the last step is passed or dismissed; whichever, the crew and budget doors (`/crew`, `/budget`) should be named in the note `endSetup` already leaves — sev: high — frames: `home-firstrun.120x40.txt` (line 27, "1 of 3"), `home-afteresc.120x20.txt` (line 7, "setting up", same question, counter gone)

9. A standing item that needs a person is drawn twice on the phone, against that file's own stated law — /home/santosh/af-polish/internal/tui3/homephone.go:208 (`type phoneLifted` has `rows` and `errands` and **no items set**), homephone.go:228 and homephone.go:271 (both append `h.itemLine(project, view)` and record nothing), homephone.go:335 (projects skip lifted `rows` only) — homephone.go:53 states "A ROW APPEARS ONCE. A conversation lifted into `waiting on you` is not drawn again under its project… twelve rows of screen cannot" — and the frame draws `? tell me when CI goes red on master / needs your look · May I re-run the typecheck …` at rows 4-5 under `waiting on you` and again at rows 8-9 under `aforge-v2`, four of twenty-six rows on the one tier that has none to spare — fix shape: give `phoneLifted` an `items map[string]bool` keyed however `itemLine` identifies one, record in both `phoneWaiting` and `phoneRunning`, and filter in `projectBlock`'s item split the way `phoneProjects` already filters `lifted.rows` — sev: high — frames: `home-phone.50x30.txt` (lines 4-5 against 8-9)

10. **H4.** At 60 columns the tab strip collapses from seven places to one, with nothing saying the other six exist — /home/santosh/af-polish/internal/tui3/pages.go:445 (`placeTabBar`), whose ladder is exactly three rungs: every place (pages.go:446), then places with a count (pages.go:452), then `barKeeps` alone (pages.go:456) — the seven words plus `tabLead`/`tabGap` (settings.go:2298, 2306) come to 62 cells, so a 60-column window overshoots by two and falls straight past the middle rung, because on a quiet machine no place has a count; what is left is ` home` and 54 empty cells, on the row whose whole job is telling a person the program has rooms — meanwhile the row above spends its full width, so the frame has the cells and spends them on the wrong row — fix shape: add a rung between "all seven" and "one" — the same words at `tabGap` 0, or the places that did not fit named as a count (`home tasks … +5`), or the elision mark this surface already owns; and independently, the *shape* of the fix is that a bar with one chip on it should never be reachable while `width >= 40` — sev: med — frames: `home-idle.60x30.txt` (line 2), against `home-idle.80x24.txt` (line 2, all seven)

11. **H5.** The hint line is sliced mid-word instead of dropping a whole hint — /home/santosh/af-polish/internal/tui3/pages.go:1089 and pages.go:1100 (`add(" "+paintHint(fit(a.placeHint(), width-2), pal, pal.dim), nil)`) — `a.placeHint()` composes a `·`-joined sentence and `fit` truncates it as one opaque string, so 60 columns draw `type to search or start something new · ↑↓ pick · enter o…`, which promises a key and then eats it; the phone does it twice on the same frame (`↑↓ pick…`, `move it…`) — the ranked-tail machinery for exactly this already exists (`rowTail`, and `rowShort` was written for the legend's "fits whole or drops the slot" ladder, rowfit.go:388) and the hint line does not use it — fix shape: `placeHint` returns `[]rowField` rather than a joined string, and the foot draws it through `rowTail(fields, width-2)` so clauses fall off the end whole and in priority order; failing that, the cheap version is to split on `" · "` and drop trailing clauses until it fits — sev: med — frames: `home-idle.60x30.txt` (line 30), `home-phone.50x30.txt` (lines 29-30)

12. The pulse line is all-or-nothing: it drops every fact rather than the cheapest one — /home/santosh/af-polish/internal/tui3/pulse.go:118-121 (`gap := width - name - tail - 1; if gap < 1 { return name }`) — the four segments are joined into one string at pulse.go:114 and then either fit or vanish, so a window one cell too narrow loses `1 want you` (the loudest thing the surface can say) together with the clock, and there is no width at which the line says "someone is waiting" without also saying what time it is; the segments are already ranked, painted and independent (`pulseSegments`, pulse.go:134) — this is the same class as row 11 and is the reason H4 reads as "navigation outranked by a clock": at 60 columns the clock is on the frame whole while the seven rooms are not — fix shape: `pulseSegments` returns `rowField`s (`$1.85 / $500.00` → `$1.85`, `wed 11:02pm` → `11:02pm` → nothing) and the line draws them through `rowTail` at `width - name - 1` — sev: med — frames: `home-idle.60x30.txt` (line 1, 44 of 60 cells)

13. A conversation with no title yet is drawn as a title-cased hex id — /home/santosh/af-polish/internal/tui3/resume.go:275 (`humanName`, whose last fallback is `titleCase(unpackName(sessionStem(session.File)))`), reached from home.go:4738 (`homeName`) — the row a person is *standing in* is the one most likely to have no title, so opening aforge and looking at home shows `○ 273Ecd8c7da60bc8   aforge-v2 here`; the emptiness law says an unknown draws nothing, and a capitalised identifier is worse than nothing because it reads as a name somebody chose — fix shape: when the only source left is the transcript stem and that stem is a bare hex/ULID, draw the plain word this surface would use for it (the row already carries its project and `here`, so a `new conversation` or a dim `untitled` costs nothing and lies about nothing) rather than title-casing an id — sev: med — frames: `home-open.120x40.txt` (line 18)

14. An opened fold still says how many rows it is hiding, and the two folds on this surface disagree about the words — /home/santosh/af-polish/internal/tui3/switcher.go:605 (`addFold` changes only the glyph: `mark + strings.TrimPrefix(word, GlyphCollapsed)`) against /home/santosh/af-polish/internal/tui3/home.go:4471 (`homeQuietWord`, which correctly returns `…N fewer` when open) — press `enter` on `▸ 5 more, quiet since sep 1` and the list draws twelve rows and a line reading `▾ 5 more, quiet since sep 1`: one glyph is the only thing distinguishing "five are hidden" from "five of these are the ones you asked for", and the surface already owns the right word one file over; there are also two spellings of the fold itself, `foldLine` (placeprose.go:265, `▸ 4 more`) and `homeQuietWord` (`…4 more`), which is why the phone draws `▸ …2 more` with both a mark and an ellipsis on one line — fix shape: one fold word for the surface — `addFold` calls the same speller `homeQuietWord` does and says the way back when it is open; delete the `…` prefix from `homeQuietWord` so the mark is the caller's, which is what `foldLine` already assumes — sev: med — frames: `home-idle.120x40.txt` (line 14) against `home-open.120x40.txt` (line 19); `home-phone.50x30.txt` (line 22)

15. The standing-item hint reads as one key bound to two verbs — /home/santosh/af-polish/internal/tui3/homestanding.go:86 (`homeItemActions = homeItemEnterWord + " · → " + homeItemPauseWord + " · " + homeItemStopWord`) — the foot draws `enter open where it was asked · → pause · stop · tab next place · esc close`, in which every other clause is `key verb` and this one is `key verb · verb`; a person reads `stop` as a verb with no key, tries `s`, and gets nothing (the letters are the `→` strip's, which is what the constant means and not what it says) — fix shape: say it in the grammar the card already uses for the same strip, `→ verbs: pause, stop` (`homeVerbsWord`, place_home.go), or `→ pause or stop`; either way the clause is one key naming one strip — sev: med — frames: `home-firstrun-esc.120x40.txt` (line 40)

16. The scope chip says where your typing lands as a path on one row and as a bare name on the next — /home/santosh/af-polish/internal/tui3/pages.go:1193 (`scopeWorkspace` returns `homeWhere(line)`, which is switcher.go:271's rule: "the project's own path, and its **name** when nothing recorded one") through pages.go:1177 — with the cursor on a conversation the chip reads `here ~/aforge-v2`; move it to a standing item with no recorded `ProjectDir` and the same chip reads `here aforge-v2`, which is not an address and cannot be told from a second checkout of the same name — the function's own doc comment (pages.go:1186-1192) states this is the drift the ONE SOURCE OF TRUTH law exists for, and cites the exact bug ("a person read `here ~/aforge-v2` and started a task in `~`") — fix shape: `scopeWorkspace` returns a path or nothing; when the row records no directory the chip falls back to this window's workspace (which it already does for a row with no `homeWhere`) rather than promoting a display name into the address slot — sev: med — frames: `home-idle.120x40.txt` (line 39) against `home-firstrun-esc.120x40.txt` (line 39)

17. The resting foot promises `tab next place` on a frame drawing one place — /home/santosh/af-polish/internal/tui3/home.go:256 (`homeRestHint = homeFootWord + " · tab next place"`), which is a constant and never asks the bar what it drew — at 60 columns the strip has collapsed to ` home` (row 10 above) and the foot still names the places, so the one line that could rescue the collapsed bar instead reads as a hint about rooms that are not on screen; the key itself still works, so this is a wording defect rather than a dead key — fix shape: at a width where the bar dropped words, the clause earns its cells by naming what it reaches (`tab · 7 places`) — sev: low — frames: `home-idle.60x30.txt` (lines 2 and 30)

18. The card's four readings are separated by blank rows that a short card pays for twice — /home/santosh/af-polish/internal/tui3/place_home.go:519-542 (`cardBandsOf` groups) and home.go's `homeCardStack` — at 160×24 the card draws 15 rows of content across rows 5-19 with three blank separators, then the list beside it ends at row 14 and both columns leave rows 20-21 empty above the rule; the rhythm is right and the arithmetic is right, but the two columns end at different heights with no relationship between them, which reads as one column having been cut — worth noting only because fixing row 2 (the list growing to the room) makes the columns end together and this stops being visible — fix shape: none on its own; verify after row 2 lands — sev: low — frames: `home-short.160x24.txt` (rows 14 and 19)

---

## fixed

This pass took rows 1, 2, 4, 5, 6, 7 and 9. Every fix is pinned by a named test
in `internal/tui3`, and every one was verified on a frame captured from the real
binary against the demo home — the `-after` frames sit beside the originals in
`docs/design/polish/frames/`.

**Row 1 — the product has one name, and it is `aforge`.**
`styles.go`'s `product` is now `"aforge"`; `pulse.go`'s second spelling
(`pulseName`) is deleted and the pulse line reads the one constant.
`welcome.go` grew the two letterforms the name needed (`r`, `g`), and
`firstrun.go`'s six sentences interpolate `product` instead of spelling the name
a seventh through twelfth time. Files: `internal/tui3/styles.go`,
`internal/tui3/pulse.go`, `internal/tui3/welcome.go`, `internal/tui3/firstrun.go`,
`internal/tui3/home_test.go`; manual: `starting-aforge.md`, `screen.md`,
`commands.md`, `empty-screen.md` (the wordmark's own ASCII drawing included).
Tests: `TestTheProductIsNamedOnceAndItIsTheNameYouType`,
`TestTheWordmarkCanSpellTheProductsWholeName`,
`TestTheFirstScreensWordmarkAndItsProseNameOneProduct`
(`internal/tui3/productname_test.go`). Frames:
`home-firstrun.{120x40,80x24,60x30}.txt` → `home-firstrun-after.{120x40,80x24,60x30}.txt`.

**Row 2 (H1) — the list grows to the frame, and eight is the floor.**
`switcherView` gained a `room`, handed in by `place_home.go`'s `buildSwitch`
from the room `placeHome.body` was given (less the errands standing over the
reading); `switcherReading.capAtRest` returns what is left of the frame under
what is already on it, never fewer than `switcherShown`, and the grouped view
settles its cap against the headings it will spend. `switcherShown` finally has
the doc comment it never had. The reading stays PURE — a reading given no room
draws exactly what it drew before, which is what keeps every existing fixture
true. Files: `internal/tui3/switcher.go`, `internal/tui3/place_home.go`,
`internal/tui3/home.go`; manual: `home.md` (six passages that said "eight rows").
Tests: `TestTheListGrowsToTheFrameAndIsNeverShorterThanEight`,
`TestAGrownListStillPaysForItsOwnHeadings`,
`TestHomeDrawsAsManyConversationsAsTheFrameHolds`
(`internal/tui3/switcherroom_test.go`). Frames:
`home-idle.{120x40,140x45,160x50}.txt` → `home-idle-after.{120x40,140x45,160x50}.txt`
— seventeen conversations on the 120x40 frame where twelve used to be eight and a fold.

**Row 4 — the name is whole before any fact gets a cell.**
`switcherPaintRow`'s give-way loop measures the whole title instead of an
eight-cell floor, so the facts drop until the name fits and the eight survives
only as the last resort — which is where `rowPlan.fit` stops too. The row was
NOT routed through `rowPlan`/`rowHalves`: that fitter joins its facts with
` · ` and this list joins them with a space, so the change would have re-spelled
every row on home and every needle the e2e suite waits for. The guard is the
audit's own first fix shape; the shared fitter is a wave of its own.
Files: `internal/tui3/switcher.go`. Tests:
`TestTheNameIsWholeBeforeAnyFactGetsACell`,
`TestAWideFrameKeepsTheNoteBesideTheWholeName`,
`TestANameTooLongForTheFrameTakesTheRowAlone`
(`internal/tui3/switcherrow_test.go`). Frames: `home-idle.100x30.txt` (line 6,
`? tell me when CI goe…`) → `home-idle-after.100x30.txt` (line 6, the whole name).

**Row 5 (H3a) — the card's place line spends the fact, not the address.**
`homeCardPlace` fits the address alone and hangs the repository's clause off
what is left, dropping it WHOLE rather than slicing the path's head off. Two
clauses are reserved out of the address's cells rather than ranked behind it:
`that folder is gone`, which is the statement that there is no checkout, and the
door word (`here`, `open in another window`), which is what the key under the
person's finger will do. Files: `internal/tui3/place_home.go`; manual: `home.md`
(the place line's own bullet). Tests:
`TestTheCardsPlaceLineSpendsTheFactsBeforeTheAddress`,
`TestARefusalAboutTheAddressTravelsWithTheAddress`
(`internal/tui3/cardplace_test.go`). Frames: `home-idle.200x50.txt` (line 6,
`…rge-v2 · master, 3 files dirty · open in another window`) →
`home-idle-after.200x50.txt`, `home-short-after.160x24.txt`.

**Row 6 (H3b) — a card row never prices a name it had to cut.**
`bandSidesWithSeparator` shares its row while the label fits WHOLE beside the
tail; a label that had to be cut takes the row and the fact goes to the line
under it — the two-line branch that was already there, fired by the right
question. The caller's floor survives as the last resort, for a label too long
for a row of its own, where a second line would be spent for nothing.
Files: `internal/tui3/homebands.go`. Test:
`TestACardRowNeverPricesANameItHadToCut` (`internal/tui3/bandsides_test.go`).
Frames: `home-short.160x24.txt` (line 12, `✓ Port the picker onto the ne… $0.31`)
→ `home-work-after.160x24.txt`.

**Row 7 — a short first run keeps its box and its way out.**
The setup block now gives up rows from its MIDDLE, last one first: the prose and
then the wordmark, never the question, the `›` box or the keys line. `setupTrim`
is the whole of it, and the caret rides the trim; a block still too tall after
every soft row has gone loses its HEAD rather than its foot. Files:
`internal/tui3/firstrun.go`; manual: `getting-started.md`. Tests:
`TestAShortWindowKeepsTheSetupsBoxAndItsWayOut`,
`TestATallWindowStillDrawsTheWholeSetupBlock`
(`internal/tui3/firstrun_short_test.go`). Frames:
`home-firstrun-tiny.120x12.txt` → `home-firstrun-tiny-after.120x12.txt`
(the `›` box and `enter connects in browser · paste a key · esc not now` are both
on the frame), `home-firstrun-short-after.120x16.txt`.

**Row 9 — a standing item is drawn once on the phone.**
`phoneLifted` gained an `items` set, `phoneWaiting` and `phoneRunning` record
into it, and `projectBlock` skips what the triage sections already drew — the
map is hung on the view because that block is shared with every wider frame and
is nil there. Files: `internal/tui3/homephone.go`, `internal/tui3/home.go`.
Test: `TestAPhoneInboxDrawsAStandingItemOnlyOnce`
(`internal/tui3/switcherrow_test.go`). Frames: `home-phone.50x30.txt` (rows 4-5
against 8-9) → `home-phone-after.50x30.txt` — the duplicate is gone and two
conversations came back into the project block with the rows it freed.

**Row 10 (H4) — at sixty columns the strip carries all seven places.**
Closed by the NARROW lane's FIRST row, not by this one, and recorded here because a row
closes in the audit that wrote it down. `placeTabBar`'s ladder gained a middle rung
that redraws the same seven words with the air between the chips given up (57 cells
against sixty), and under that `barWordsAt` fills in the bar's own order and ends the
row with `barMoreWord`'s `+2 more`. Files: `internal/tui3/pages.go`. Tests:
`TestTheNarrowBarStillSaysWhereElseYouCanGo`,
`TestABarTooNarrowForEveryWordSaysHowManyItDropped` (`internal/tui3/narrow_test.go`).
Frames: `home-idle.60x30.txt` (line 2, `  home` and 54 empty cells) →
`keep-home.60x30.txt` (line 2,
`  home  tasks  standing  memory  spend  search  settings`), captured this pass from
`bin/aforge`; the rung under it on `narrow-tasks-after.48x24.txt` (`… spend  +2 more`)
and `narrow-tasks-after.56x24.txt` (`… search  +1 more`), and the wide tier unmoved on
`keep-home.{80x24,120x40,160x50}.txt`.

**Row 11 (H5) — the foot drops whole hints and never slices one.**
Closed by the narrow lane's SECOND row. `hintFit` keeps a ranked `·` ladder and grows a
second one under it for a foot that is a SENTENCE, dropping the bracketed gloss first
and the ` — ` elaboration second; `placeMsgLine` and the phone's four foot sites go
through it, and the law is pinned structurally — `paintHint` may not be handed a bare
`fit` call, read off the tree with `go/parser`. Files: `internal/tui3/pages.go`,
`internal/tui3/homephone.go`. Tests: `TestTheNarrowFootDropsWholeHintsAndNeverSlicesOne`,
`TestEveryHintIsFittedByDroppingClausesNotByCuttingCharacters`
(`internal/tui3/narrow_test.go`). Frames: `home-idle.60x30.txt` (line 30,
`type to search or start something new · ↑↓ pick · enter o…`, a key named and then
eaten) → `keep-home.60x30.txt` (line 30,
`type to search or start something new · tab next place` — `↑↓ pick` and `enter open`
dropped WHOLE), with every clause still on `keep-home.120x40.txt` (line 40).

**Row 12 — the pulse gives way one segment at a time, and the clock goes first.**
Closed by the pulse lane (audit-narrow's SIXTH row). `pulseLine` was
`gap := width - name - tail - 1; if gap < 1 { return name }` — everything, or the name
alone. It walks `app.pulseRungs` now, widest rung first, so what a narrow frame shows is
a SUBSET of what a wide one shows; the rank is `clock → allowance → spend → moving →
want you`, and the allowance is dropped by RESPELLING the money clause rather than by
printing a figure the line had given up on, so no rung can draw `$0.00`.
Files: `internal/tui3/pulse.go`. Frames: `home-idle.60x30.txt` (line 1, the joined
string that fits whole or vanishes whole) → this pass's ladder, captured at four widths
from the real binary: `keep-pulse-40.40x24.txt` (line 1,
` aforge     $123.45 / $500 · thu 2:20am`) → `keep-pulse-34.34x24.txt` (line 1,
` aforge            $123.45 / $500` — the clock alone has gone, the money is still
there) → `keep-pulse-28.28x24.txt` (the same money clause at 28 cells). The old line
would have drawn ` aforge` and nothing else at 34.

**Row 13 — a conversation nobody has named is called `new conversation`.**
Closed by the narrow lane's THIRD row. `listName` (`internal/tui3/names.go`) is home's own
ladder — the title it settled on, then the folder's name only where `idShaped` says it
reads as words, then the plain word this surface already uses for the fact — and
`homeName` is one call to it, so home, the card, the phone's inbox, the switcher and
home's filter all say the same thing. `idShaped` is `readableName`'s own guard asked as
a question, so the two can never disagree about whether a string is a name or an id.
Files: `internal/tui3/names.go`, `internal/tui3/home.go`. Test:
`TestAConversationWithNoTitleIsNamedInWordsNotHex` (`internal/tui3/narrow_test.go`).
Frames: `home-open.120x40.txt` (line 18, `○ 273Ecd8c7da60bc8   aforge-v2 here`) and
`narrow-home.60x30.txt` (line 24, `○ 927D303242f9d00e`) → `narrow-home-after.60x30.txt`
(line 24, `○ new conversation     aforge-v2 here`); and no title-cased hex on any frame
captured this pass — `keep-home.{60x30,80x24,120x40,160x50}.txt`.

**Row 17 — the resting foot names places that are on the frame.**
Recorded closed rather than fixed, which is what this row asked for: `homeRestHint`
(`home.go:256`) is still the constant `… · tab next place`, and after row 10 there is no
width at which that clause names rooms the frame is hiding. At sixty columns the bar
carries all seven words and the foot names the key that walks them; under sixty home is
the phone tier, which draws its own head and no place bar and no such clause. Frames:
`home-idle.60x30.txt` (lines 2 and 30, one place named and the foot promising seven) →
`keep-home.60x30.txt` (line 2 all seven, line 30 `… · tab next place` whole), with
`keep-home.50x24.txt` and `keep-home.44x24.txt` showing the phone tier's own head
(` home … esc close`) and a foot of ` enter open` — no place clause on either. This is
`N8` in audit-narrow, closed there in the same terms.

### skipped, and why

The numbers below are SPELLED AS WORDS on purpose: `scripts/ledger.py`
reads `Row <digit>` anywhere under `## fixed` as a closure, and every row in
this list is one that is NOT closed.

- **Row three (H2)** — moving `homeSwitchFull` from 120 to ~96 so the card arrives
  near 136 columns. The arithmetic is a layout call the audit's fix shape
  proposes but does not settle: it moves a tier boundary the whole home is laid
  out against (`homebridge_test.go` pins that ladder width by width), and it is
  entangled with row 4 — the note now gives way to the name, so how many cells a
  row still wants at 120 has changed under the audit's measurement. It wants its
  own pass, with frames at 130, 136 and 140.
- **Row eight** (`esc not now` ends first run for good) — the audit itself offers two
  fixes with different meanings: rename the key, or stop writing `setup_seen_at`
  until the last step. Which one is right is a product decision about whether the
  crew and budget questions are ever asked again, and neither the audit nor the
  code settles it. Note that `setupKeysWord` already says `esc skips setup` on
  four of its six branches, so whoever takes this row is reconciling two
  spellings as well as the behaviour.
- **Rows 10, 11 and 12** were skipped by this lane — `pages.go` and `pulse.go`'s
  segment ladder, held by the hint-line and narrow lanes this wave. They have
  since landed and are recorded above.
- **Rows 13 through 18** — sev: med and low, and outside this pass's brief. Row
  eighteen (the card and the list ending at different heights) should be re-read now
  that row 2 has landed: the two columns end together on the `-after` frames.

### what other lanes should know

- **The list is settled by the DRAW.** `placeHome.body` hands the room to the
  reading and rebuilds when it moves, so `a.home.lines` is the list of a window
  with no height until one frame has been drawn. A test that opens home and reads
  the lines without drawing is reading the old shape; `switchLab.open` draws one
  frame for exactly this reason.
- **A fold now hides what the frame could not have shown anyway.** Opening it
  puts the rows on the list, where `↓` reaches them, and not necessarily on the
  visible frame — assertions that opened a fold and looked for a row on the
  frame were rewritten to look at the list.

### tests that pinned the old cap, and what they say now

Seven existing tests asserted the eight-row ceiling or the joined place line.
None of them was weakened; each was given the frame its subject actually needs,
and the change is stated in the test's own comment.

| test | what changed |
| --- | --- |
| `TestHomeFoldsTheWholeMachinesQuietTailBehindOneDoor` | 100×17, where the floor is what is left, so the eight it is about are the eight it gets |
| `TestTheOneFoldOpensAndFoldsOnEveryGesture` | 100×17, and the row it follows is the FIRST behind the fold |
| `TestTheOneFoldOpensAndClosesOnTheArrows` | 120×19, and the hidden row is looked for on the list rather than on the frame |
| `TestHomesOneFoldIsADoorBothWays`, `TestTheFoldLeavesTheCursorOnTheLineThatOpenedIt` | 120×19 (`switchLab.open` now draws one frame, so the lines are the frame's) |
| `TestAFreshLaunchOpensOnTheFirstConversationWhenItsOwnIsNotListed` | 200×17 — the card tier, and short |
| `TestTheRightArrowIsNotSeizedOnARowWithNoVerbs` | 120×17, with one frame drawn before the lines are read |
| `TestAMatchBehindTheCollapseIsFoundAnyway` | 100×17 |
| `TestHoveringAnotherProjectsRowReadsThatProjectsRepository` | the card is drawn wide enough for a forty-character temporary directory AND a branch; the drop at 36 cells is the layout law, not a lost reading |

---

## fixed — the home lane's second pass

Frames prefixed `home2-` were captured from `bin/aforge` in a real terminal on
socket `polish-home`, against a demo home seeded this pass. Every fix below was
REVERTED and its test watched to fail before the fix was put back; where a test
did not fail on the revert it was rewritten until it did, and the two that could
not be made to discriminate are named as such.

**Row 14 — home's two folds say one sentence.** There were two writers:
`switcher.go`'s `addFold` handed a finished `foldLine` and swapping the glyph on
the front of it (`▸ 4 more, quiet since sep 1`), and `home.go`'s `homeQuietWord`
building its own (`…7 more, quiet since 3h`) — a leading ellipsis on one and not
the other, and a calendar date against an elapsed span. `placeprose.go` now holds
the one speller: `foldWords(open, n, clause)` says the count and, when the fold
is open, says `N fewer` instead of `N more`; `foldLine` is that word wearing the
shut mark, for the five folds that are only ever shut; `quietFoldClause` is the
one spelling of the age. **THE ELAPSED SPELLING WON, and it is `sinceAt`** — the
same ladder every row's own age is drawn with, so a fold and the rows it stands
over can be read against each other instead of asking a person to convert
between two units; `sinceAt` reaches for a calendar date by itself past thirty
days, which is exactly when the elapsed form stops being readable, and an
unconditional `Jan 2` was drawing today's date over rows three hours old. The
`…` is gone: the mark belongs to whoever draws it, which is what let the phone
draw `▸ …2 more`.
Files: `internal/tui3/placeprose.go`, `internal/tui3/switcher.go`,
`internal/tui3/home.go`; manual: `home.md`, `keys.md`, `commands.md`.
Tests: `TestBothOfHomesFoldsSpellTheirCountAndTheirQuietOneWay`,
`TestAnOpenedFoldOnHomeSaysHowManyItWouldTakeAway`
(`internal/tui3/foldword_test.go`).
Reverted, each half separately: reverting `switcher.go` alone prints
`▸ 5 more, quiet since aug 23` against `▸ 5 more, quiet since 9d`; reverting
`homeQuietWord` alone prints `…5 more, quiet since 9d`. Two existing tests
pinned the old shape and were moved onto the new law with the reason in their
comments — `TestTheOneFoldOpensAndFoldsOnEveryGesture` wanted `▾ 5 more` over
five rows a person could see, and `TestTheSwitcherFoldsOnlyTheQuietTailAndCanHideIt`
wanted `aug 20`.

**Row 3 (H2) — the card arrives at 136 columns, not 160.**
`homeSwitchFull` was 120, which was the list measured WITH the row's note on it
— and the note is the fact the card exists to carry, so the layout reserved the
list's width in order to draw, badly, the sentence the card would have drawn
properly. It is now an addition of its parts: `homeSwitchName` (70, the longest
name the list undertakes to draw whole) plus `homeSwitchTail` (26, the mark, the
two cells after it, a project tag and an age, each with the space
`switcherTailWidth` puts in front of it) = 96, so `homeCardMin` is 136. The note
is ranked first out of a row by `switcherPaintRow`'s own ladder, which is the
mechanism: past 136 the note gives way and the card takes it up, and the name,
the tag and the age are untouched at every width the tier exists at.
Files: `internal/tui3/homebridge.go`; manual: `home.md` (the 160-column rule and
eight passages that repeated it), `keys.md`, `asking-from-home.md`.
Tests: `TestTheCardArrivesOnAnEverydayWindowAndNotOnlyOnAHugeOne`,
`TestBesideACardARowStillKeepsItsNameItsProjectAndItsAge`,
`TestTheListNeverDropsBelowWhatItUndertookToDraw`
(`internal/tui3/cardtier_test.go`). Reverted to 120: the first prints
`a 136-column frame is tier 0`, the second `a 140-column frame drew a card of 0
cells`, the third `homeSwitchFull is 120, want 96`.
Frames: `home2-before.120x40.txt` and the audit's own `home-idle.140x45.txt` (no
card) → `home2-after.140x45.txt`, where the card stands beside the list on an
ordinary laptop window, and `home2-after.{60x30,80x24,120x40,160x50}.txt` for
the tiers either side.

**Row 8 — `esc` is named for what it does, and the questions it retires get a
door.** The audit offered two fixes with different meanings; this pass took the
one the audit itself argues for — *the code is deliberate and the label is the
defect*. `setupSkipKeysWord` is now the one spelling of that key on all six feet
(it was `esc skips setup` on five and `esc not now` on the browser-connect step,
which is the FIRST screen a new install sees, and which reads as a promise that
the question comes back). And `endSetup` leaves one dim line naming the doors
onto the questions esc walked past — the step on screen and the ones under it,
never one already answered: `still yours to set · /crew picks the five models
aforge works with · /budget sets what it may spend`. A finished flow leaves no
such line at all.
Files: `internal/tui3/firstrun.go`; manual: `getting-started.md`.
Tests: `TestEveryStepOfSetupNamesEscForWhatItDoes`,
`TestSkippingSetupNamesTheQuestionsItRetired`,
`TestFinishingSetupLeavesNoLineAboutQuestionsItAsked`
(`internal/tui3/setupskip_test.go`). Reverted: the first prints
`step 0's foot reads "enter connects in browser · paste a key · esc not now"`,
the second that the note left behind never names `/crew` or `/budget`.
NOT taken: leaving `setup_seen_at` unwritten until the last step. That is a
product decision about whether the crew and budget questions are ever asked
again, and neither the audit nor the code settles it.

**Row 15 — one key, one clause, on both feet that name the `→` strip.**
`homeItemActions` read `enter open where it was asked · → pause · stop`, in which
`stop` is a verb with no key — the letters are the strip's and only exist once
`→` has drawn it. `homeStripWord` is the one speller now and says it in the
grammar the card beside it already uses (`homeVerbsWord`): `→ verbs: pause,
stop`. The standing place's own foot had the same fault with three verbs and
goes through the same function.
Files: `internal/tui3/homestanding.go`, `internal/tui3/place_standing.go`;
manual: `home.md`, `standing-orders.md`.
Tests: `TestAnItemsFootNeverOffersAVerbWithNoKey`,
`TestAFootNamesTheStripInTheCardsOwnGrammar`
(`internal/tui3/stripword_test.go`). Reverted: the first prints
`enter open where it was asked · → pause · stop`, naming the clause `"stop"`.

**Row 16 — the scope chip is an address or it is this window's.**
`scopeWorkspace` was reading `homeWhere`, which answers a different question
correctly — `ctrl+t` wants a bucket, and a project's NAME is a fine bucket key —
and wrongly here: the chip says where a sentence will LAND. `scopeAddress` takes
a conversation's `ProjectDir`, a standing item's `standing.Item.Workspace`, or a
project row's `Path`, and answers nothing where a row records none, which falls
through to this window's own workspace exactly as it already did for a row with
no project at all.
Files: `internal/tui3/pages.go`.
Tests: `TestTheScopeChipTakesARowsAddressAndNeverItsName`,
`TestTwoHomeRowsSayWhereInTheSameWords` (`internal/tui3/scopeaddress_test.go`).
Reverted: the second prints `the item's chip reads "here aforge-v2" — a name in
the slot that says where a task will land; the row above it reads "here
/w/aforge-v2"`. (The first is a pure-function law and does not discriminate on
that revert; the second is the one that pins the seam.)

**Row 18 — recorded closed by VERIFICATION, which is what this row asked for.**
No code change: the audit's own fix shape is "none on its own; verify after row 2
lands". It has. On `home-short.160x24.txt` the list ended at row 14 and the card
at row 19, five rows apart, which read as one column having been cut; on
`home2-short.160x24.txt` the list runs to row 19 and the card to row 20, and both
leave the same blank row above the rule. Confirmed in the reading as well as on
the frame: at 136×24, 160×24 and 160×50 the two columns end on the same row or
one apart.
NO TEST SHIPS WITH THIS ROW, and that is deliberate rather than an omission. The
two laws that could be written here are either row 2's own — already pinned by
`TestTheListGrowsToTheFrameAndIsNeverShorterThanEight` — or a claim about two
columns ending together, which is content-shaped and passed against a reverted
`capAtRest` on every fixture tried. A test that cannot be made to fail against
the defect it names is worse than none.
