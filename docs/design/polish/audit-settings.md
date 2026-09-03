# audit — settings, memory, spend

Audit lane, `ui/polish-v0`. Read-only: no source touched. Every row below was
reproduced against `bin/aforge` on the demo home through `scripts/frame.sh`
(socket `polish-set`), at 160x50, 120x40, 80x24 and 60x30, walking all nine
settings tabs and opening editable values with `enter`.

Frames are under `/home/santosh/af-polish/docs/design/polish/frames/`, prefixed
`set-`.

**Checked and found already right — no defect filed:** no `lipgloss.Color("…")`
literal in any of the eleven files on this surface (every ink goes through the
theme role map, and styles.go carries the light-terminal contrast table);
memory's hint line is genuinely per-row (`place_memory.go:847`); the settings
Spending tab obeys the emptiness law (`todayReading` returns nil before the
first call of the day, `settingspend.go:123`); the spend place obeys it too
(`spendNothingWord`, `spendplace.go:465`); `/budget` writes through the same
registry row the tab writes through.

---

 1. The top line's `today` figure disagrees with every other reading of the same fact on the same frame — `$1.85 / $500.00` on the pulse over `today $0.13 of $500` in the body — because the pulse sums task-record costs plus standing runs while the spend place and the Spending tab both read the usage ledger — `internal/tui3/homemachine.go:214` (`machineSpentToday`) vs `internal/tui3/settingspend.go:347` (`readDayCost`) vs `internal/tui3/spendplace.go:405` (`todaySpend`) — a developer reading their own bill gets two answers 14x apart against one denominator and cannot tell which is the money; this is exactly the "one source of truth" law, and the figure people act on — `machineSpentToday` walks only `a.home.everyProject()`, so work in a project home has not scanned is invisible to it — `readDayCost`'s ledger walk is the only reading that sees every call; make `machineSpentToday` call it (or delete `machineSpentToday` and have `machineFacts.spent` read `a.dayCost` on the same beat the panel does) so one function answers "what has this machine spent today" — sev: high — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-pulse-spend4.120x40.txt`, `/home/santosh/af-polish/docs/design/polish/frames/set-set-spending.120x40.txt`, `/home/santosh/af-polish/docs/design/polish/frames/set-spend.80x24.txt`

 2. The same top line shows a different figure depending on which places you have walked through — `$1.85 / $500.00 · 1 want you` on home, spend, search and settings, `$0.37 / $500.00` with no `want you` clause on tasks, standing and memory — because the pulse reads the home SCREEN's view model, which `dropHome` empties the moment you leave home — `internal/tui3/pulse.go:135` → `internal/tui3/homemachine.go:98` reads `a.home`, wiped by `internal/tui3/home.go:1156` (`a.home = homeView{}`), and quietly re-primed from another room by `internal/tui3/place_standing.go:812`; the 3-second cache at `homemachine.go:100` is why the change lags the room by one or two Tabs — a developer watching the day's spend on a task page is reading standing-run spend alone (`$0.37`) and is told nothing is waiting on them when something is; the number is a function of navigation history, which is the one thing it must not be — move the machine reading off `homeView` onto `app` (an `app.machine`/`app.machineAt` pair with its own clock, primed in `placeFrame` before the pulse is drawn) so no place's `close` can take it away and no place has to re-prime it by hand — sev: high — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-pulse-home.120x40.txt`, `set-pulse-tasks1.120x40.txt`, `set-pulse-standing2.120x40.txt`, `set-pulse-memory3.120x40.txt`, `set-pulse-spend4.120x40.txt`, `set-pulse-search5.120x40.txt`, `set-pulse-settings6.120x40.txt`, `set-pulse-tasks1-slow.120x40.txt` (all in that directory)

 3. Settings rows draw a bare number whose unit exists only in the one sentence under the row you happen to be standing on — `ssh reuse 300`, `ssh heartbeat 3`, `approval countdown 10`, `background after 30`, `compact at 60`, `answer room 65536`, `working set 160000`, `context reuse 250`, `tenure after 3`, `memory floor 1536`, `busy machine 1.5` — `internal/tui3/settings.go:2494` (`value := item.row.Value()`, drawn raw) over `internal/config/settings.go:1698,1707,1892,2035,2046,2055,2065,2097,2196,2204,2212` (all `Kind: SettingCount`, all `read:` returning `strconv.Itoa`) — 300 what: seconds, connections, kilobytes? A developer scanning a tab cannot decide any of eleven rows without moving the cursor onto each one and reading a paragraph, and the panel's own law is that it shows ONE description at a time; meanwhile the two `SettingDuration` rows beside them (`quiet before practice 20m`, `arrival brief after 4h`) read perfectly, which is the shape the fix already has in the tree — give `config.Setting` a `Unit` string (`"s"`, `"tok"`, `"%"`, `"MB"`, `"a firing"`) that `rowLinesWithin` appends to the value, or retype the seconds rows as `SettingDuration` and the percent rows as the `SettingPercent` kind that already exists at `internal/config/settings.go:40` and is used by nothing — one source, and the edit box (row 5) gets it free — sev: high — frames: `set-session.120x40.txt`, `set-context-80.80x24.txt`, `set-safety.120x40.txt`, `set-tasks-tab-80.80x24.txt`, `set-settings.160x50.txt`

 4. The settings tab strip never scrolls, so on an 80- or 60-column terminal standing on Providers or Connections NO tab is highlighted at all — the strip still starts at `Session` and is cut after `Provide…` — `internal/tui3/settings.go:2343` (`sheetTabBar` builds all nine chips from index 0 and, on overflow, does `fit(line, width)` at line 2359 — a left-anchored cut that never consults `active`) — a developer on a laptop split pane presses `→` into a room and the screen stops telling them where they are; the two tabs that hold every third-party account are also invisible, so nobody discovers them — window the chips around `active` the way the PLACE bar already does (at 60 it collapses to the current word alone: `  settings`, `  memory`), marking the cut with a leading `…` — sev: high — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-connections-80.80x24.ans` (no highlight anywhere on the strip), `set-tasks-tab-80.80x24.ans`, `set-context-80.80x24.ans`, `set-settings.60x30.txt`

 5. Opening a settings value that is empty leaves the composer's own resting sentence in the box: editing `per conversation` (value `no limit`) prompts `› say what you want done` where a dollar amount must be typed — `internal/tui3/pages.go:1249` (`placeRestWord()`) is asked for the resting word whatever the box has been commandeered for, and `internal/tui3/place_settings.go:122` hands the sheet's value editor to that same box — a developer is invited to write prose into a money field and is told nothing about what the row accepts; `config.Setting.Accepts()` at `internal/config/settings.go:1277` already spells the right sentence for every kind ("an amount in dollars, like 5 or 2.50 — or none for no limit") and nothing reads it here — when `a.sheet.edit != nil`, have the box rest on that row's `Accepts()` instead of `placeRestWord` — sev: high — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-edit-perday.120x40.txt` (compare `set-edit-sshreuse.120x40.txt`, where the box holds a value and reads correctly)

 6. The selected row's description is cut at two lines with no ellipsis, so it ends mid-clause: `…new work waits for midnight or` at 60 cells, `…none removes` at 80 — `internal/tui3/settings.go:2422` (`for n, line := range wrap(about, width-6) { if n >= 2 { break } }`) — a developer reading what a limit does gets a sentence that simply stops, which reads as a rendering fault rather than as an omission and sends them to the source to find the rest — either let the description take a third line (it is drawn only for the cursor row and the panel has the room at every tier captured here) or fit the last shown line so it ends `…` — sev: med — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-set-spending.60x30.txt`, `set-set-spending.80x24.txt`

 7. Memory rows separate their fields with a double space while every clause built for the same rows joins with ` · ` — `▾ you · 6  mostly facts · 6 new today` and `· Ships on Fridays  fact  let go` — `internal/tui3/memoryplace.go:320` (`middle = "  " + middle`), `:330` (`note = "  " + note`) and `:333` (`note += "  " + line.help`), against `memoryTypeLegend`/`memoryShelfNote`/`memoryHelp`/`memoryCounts` at `:209,:225,:261,:356` which all join with ` · ` — one line carries two separator grammars, so a developer reading `· Ships on Fridays  fact  let go` cannot tell where the memory's text ends and the machine's facts begin (the comment at `:326` records that these two spaces were themselves the fix for a worse run-together, so the column idea is deliberate — the grammar just was not carried through) — join label, kind and help with `rowSep` like the rest of the surface, and keep the age right-aligned where it is — sev: med — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-memory.160x50.txt`, `set-memory.120x40.txt` (as `memory.120x40.txt`), `set-memory.80x24.txt`

 8. The spend page stacks two dollar totals with nothing tying the second to a range: `today $0.13 of $500` then `$2.05 · 326.5k tokens`, the second labelled only by a date span 60 cells away between the window arrows — `internal/tui3/spendplace.go:296` and `:297` (`railsRow` then `windowHeaderRow`), the span living in `placeHeadRow`'s right field — a developer reads two money figures in two lines and has to work out that one is today and one is a fortnight; at 160 cells the label and the figure are 145 cells apart, which is not a label — put the span (or the word `fortnight`) in front of the figure it belongs to, or move the range total onto the sparkline's own axis where the days are; the design note at `:419` argues the control should carry the span, which is fine — what is missing is any word at all beside `$2.05` — sev: med — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-spend.160x50.txt`, `set-spend.120x40.txt`, `set-spend.60x30.txt`

 9. The sparkline is fourteen cells wide at every frame width — 9% of a 160-cell line — and its axis puts a date on the left against a value on the right (`aug 20` … `today $0.13`), where that value is the same number the line two rows above already gave — `internal/tui3/spendplace.go:485` (`sparkline` writes one cell per day and nothing scales it) and `:299-313` (left is `r.days[0].Label`, right is `"today " + spendMoneyWord(day.USD)`) — a developer cannot read a fortnight's rhythm out of fourteen braille cells at the far left of a wide terminal, and an axis whose two ends are different kinds of thing teaches nothing about the shape above it; the repeated `$0.13` also breaks one-source-of-truth in the smallest possible way — scale the sparkline to the frame (n cells per bucket, floor 1) and make both axis ends dates (`aug 20` … `today`), leaving the money to the rails row that owns it — sev: med — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-spend.160x50.txt`, `set-spend.120x40.txt`, `set-spend.80x24.txt`

10. The spend place draws no hint line of its own, so its foot advertises the conversation's keys — `enter talk about it · alt+enter send it off as a task · alt+. for the map · tab next place` — while `enter` there opens the row's subject, `→` opens `b the limits`, and `shift+←/→` and `shift+↑/↓` move the window and the grain — `internal/tui3/place_spend.go:466` (`type placeSpend struct{ placeBase }` with no `hint` method) falling through to `internal/tui3/pages.go:286` (`func (placeBase) hint(a *app) string { return placeHintWords }`); compare `place_tasks.go:1024`, `place_memory.go:847`, `place_settings.go:151`, which all answer for themselves — a developer standing on the money page is told about a verb that is not the one `enter` performs and is never told the four keys that are; "key hints true for the place you are on" is the law — give `placeSpend` a `hint` naming what the cursor's row can be asked for, ending in `placeHintTail` (the search place inherits the same default and has the same fault) — sev: med — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-pulse-spend4.120x40.txt`, `set-spend.80x24.txt` (compare `set-pulse-tasks1.120x40.txt`)

11. The daily rail is spelled two ways on one frame — `$500.00` on the pulse, `of $500` in the spend body and `$500 · $0.13 today` on the Spending tab — `internal/tui3/pulse.go:166` (`dollars(facts.ceiling)`) against `internal/tui3/settingspend.go:167` (`railFigure`), whose own doc says `$500.00` "is that figure with two cells of noise on the end: nobody sets a daily limit of five hundred dollars and no cents" — the surface has one law for how a LIMIT is written and the most-read line on it does not follow the law, so a developer sees the same setting in two spellings and wonders whether they are two settings — call `railFigure` for the denominator; `dollars` stays for the numerator, which is a measurement — sev: med — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-pulse-spend4.120x40.txt`, `set-set-spending.120x40.txt`

12. `dollars` renders a positive cost under a hundredth of a cent as `$0.0000` — the status line under a memory conversation shows `$0.0000 · 5.3k/1.3M` — `internal/tui3/app.go:7596` (`case usd < 0.01: return fmt.Sprintf("$%.4f", usd)`) — a figure four decimals wide that still reads as zero is the emptiness law's own worst case: the status line's `$0.00` exception exists so segments do not jump sideways, and this is three cells WIDER than the exception while saying the same nothing; a developer watching a cheap turn is shown a zero for money that was spent — this surface already owns the right word (`spendMoneyWord`, `spendplace.go:640`, says `under a cent`) — floor the spelling so it never renders all zeros: `under a cent` where prose fits, `$0.0001` where a fixed-width segment does not — sev: med — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-memory-shelf.120x40.txt`

13. At the widths where the memory type legend fits but the line is tight, the section's IDENTITY is truncated to keep a secondary legend whole: at 80 cells the heading reads `shelves · …` beside `fact 7 · preference 2 · decision 2 · correction 1 · project state 1` — `internal/tui3/memoryplace.go:370` (`memoryJoin` computes `room := width - width(plainRight) - 2` and then `fit(left, room)`; the right field has no shorter spelling and is never shortened) — this inverts rowfit's first law ("the identity is whole or the row is pointless"): a developer at 80 columns loses the word that says what the section is, to keep a count of correction memories; at 60 the legend is dropped whole and the heading reads fine, so it is only this middle band that is wrong — build the legend as ranked `rowField`s and lay the row out with `rowTail`/`rowAll` like the rest of the surface, so the kinds fall off the end one at a time and `shelves · biggest first` never loses a cell — sev: med — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-memory.80x24.txt` (compare `set-memory.60x30.txt` and `set-memory.160x50.txt`)

14. The four ssh rows sit under `Session`, while a tab literally named `Connections` sits four tabs to the right holding Amplitude, PostHog and Braintree — and the registry files those same rows under `CategoryInterface`, which agrees with neither — `internal/tui3/settings.go:229,234,239,244` (`tab: tabSession`) against `internal/config/settings.go:2196,2204,2212,2220` (`Category: CategoryInterface`) — a developer whose `--host` link keeps dropping opens `Connections`, finds a catalog of SaaS accounts, and concludes the transport is not configurable; the word is doing two jobs on one screen — either rename the accounts tab to what its rows are (`Accounts` — every row on it is a sign-in or a key) and give the ssh rows the freed `Connections`, or leave them on Session under a `remote machines` heading so the group is at least named; whichever, make the registry category and the tab agree — sev: med — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-session.120x40.txt`, `set-connections.120x40.txt`

15. A row whose label and tail exactly fill the frame collides into one sentence with a single space between them: `per task no limit of its own · it spends against the day and this conversation` at 80 cells — `internal/tui3/rowfit.go:57` (`rowGutter = 1`) — every other row on the tab shows a wide gutter, so at this one width the eye reads the label as the first two words of the value and the column structure disappears; a developer skimming for `per task` does not find it as a row — raise `rowGutter` to 2, matching the two-cell lead the rest of this surface indents with, so a tail that cannot leave two cells drops its last field instead — sev: low — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-set-spending.80x24.txt` (compare the same row at `set-set-spending.120x40.txt` and `set-set-spending.60x30.txt`, which both read correctly)

16. On a wide frame a settings row puts 150 blank cells between a label and a three-character value — `ssh reuse` … `300` at 160 cells — `internal/tui3/settings.go:2494` → `internal/tui3/palette.go:886` (`overlayLines` right-aligns the tail to the full frame width, with no measure cap) — a developer on a full-screen terminal loses the line between the two halves of a pair and reads the wrong value against the wrong row; this is a pure-width problem and does not appear at 120 — clamp the row's measure the way prose is clamped (lay the pair out inside a max column of roughly 100 cells and leave the rest of the frame empty), which also gives the ssh and Context rows their unit (row 3) somewhere to sit — sev: low — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-settings.160x50.txt`, `set-memory.160x50.txt`

17. The `loudest day` line right-aligns a bare noun with no verb and nothing joining it to the sentence: `aug 23 was the loudest day — $0.29, rebuild-the-frame-budget-report` … `tasks`, and at 60 cells the ellipsis abuts it: `rebuild-the-frame… tasks` — `internal/tui3/spendplace.go:513` (`door = "tasks"`, handed to `spendSides` as the right field) — the word is meant as a door label but reads as a fourth fact about the day, and a developer cannot tell whether `tasks` is a count, a category or a place; nothing else on the surface labels a door with a bare noun — say the door as the rest of the surface says one (`enter opens it in tasks` where it fits, `in tasks` where it does not), or drop it and let the place's hint line (row 10) carry `enter` — sev: low — frames: `/home/santosh/af-polish/docs/design/polish/frames/set-spend.120x40.txt`, `set-spend.60x30.txt`, `set-spend.160x50.txt`

---

## fixed

Fix lane, `ui/polish-v0`. Frames re-captured against `bin/aforge` on the demo home
through a private tmux socket (`polish-fixset`), walking all nine settings tabs at
160x50, 120x40, 80x24 and 60x30. Before frames are untouched; every after frame carries
an `-after` suffix.

**Row 3 — every settings row draws what its number means.** `internal/config/settings.go`
grew `Setting.Unit`, `Setting.UnitOne`, the `UnitInLabel` sentinel and `Setting.Reading()`
— the unit lives beside the default, so it is written once and every surface that draws a
number gets it. `Setting.Apply` now takes the unit back off what was typed, so a row that
draws `300s` accepts `300s`. `internal/tui3/settings.go`'s `rowLinesWithin` draws
`Reading()` where it drew `Value()`; the edit box still opens on the bare figure. Fifteen
rows carry a unit now: `ssh reuse 300s`, `ssh heartbeat 3s`, `approval countdown 10s`,
`background after 30s`, `task countdown 15s`, `compact at 60%`, `answer room 65536 tok`,
`working set 160000 tok`, `context reuse 250%`, `memory floor 1536 MB`, `busy machine 1.5
per core`, `tenure after 3 clean firings` — and `task repair rounds`, `tasks at once` and
`ssh missed heartbeats` declare `UnitInLabel`, because their own label already names what
is counted. The `context reuse` hint stops saying "in hundredths" and names the whole the
percentage is of; the `compact at` hint stops saying "as a percent", which the value now
says.
Files: `internal/config/settings.go`, `internal/tui3/settings.go`,
`internal/manual/chat/commands.md`.
Tests: `TestEverySettingSaysWhatItsNumberMeans`, `TestEverySettingSaysWhatItDecides`,
`TestAUnitIsWrittenAgainstItsNumberOnlyWhenItIsASymbol`, `TestASettingTakesBackTheUnitItDrew`
(`internal/config/settingunit_test.go`); `TestEverySettingRowDrawsWhatItsNumberMeans`
(`internal/tui3/settingunit_test.go`).
Frames: `frames/set-context-after.120x40.txt`, `set-workspace-after.120x40.txt`,
`set-safety-after.120x40.txt`, `set-tasks-after.120x40.txt` — against `set-context-80.80x24.txt`,
`set-session.120x40.txt`, `set-safety.120x40.txt`, `set-tasks-tab-80.80x24.txt`.

**Row 4 — the tab strip follows the cursor.** `sheetTabBar` no longer builds from index 0
and cuts on the right. `tabWindow(width, active)` picks the chips the frame can hold with
the cursor's chip always among them, and each cut end wears a `…`; `tabSpans` and
`tabAtColumn` take the same window, so a click resolves against the strip that was
actually painted and a tab the window left out cannot be clicked. Scrolling rather than
the place bar's collapse, and the comment over `tabWindow` argues it: nine tabs are one
ordered strip that `←` and `→` walk a step at a time, so dropping the middle would make
those two keys jump between words that are not neighbours.
Files: `internal/tui3/settings.go`, `internal/tui3/chip_test.go`,
`internal/tui3/chrome_test.go`, `internal/manual/chat/commands.md`.
Tests: `TestTheSettingsTabStripAlwaysInksTheTabYouAreStandingOn`,
`TestTheSettingsTabStripMarksTheTabsItCouldNotShow` (`internal/tui3/settingunit_test.go`).
Frames: `frames/set-providers-after.80x24.ans` and `set-connections-after.80x24.ans` now
ink the open tab (`48;5;237` band on `Providers` / `Connections`) where
`set-connections-80.80x24.ans` and `set-tasks-tab-80.80x24.ans` inked nothing; every tab at
`set-*-after.60x30.ans` is inked too.

**Row 5 — partly fixed, and the last line is another lane's.** The line above the box now
says which row is being changed AND what it takes, in the writer's own words: `per
conversation · an amount in dollars, like 5 or 2.50 — or none for no limit`. That is
`sheetEditNote` in `internal/tui3/settings.go`, feeding `sheetEdit.label`, which
`place_settings.go`'s `note` already draws. **The box itself still rests on `say what you
want done`**: the only seam for that is `app.placeRestWord` at `internal/tui3/pages.go:1282`,
which this lane does not hold. One line there — consult `a.sheet.edit` and answer with the
row's `Accepts()` — finishes it.
Files: `internal/tui3/settings.go`, `internal/manual/chat/commands.md`.
Test: `TestOpeningAValueSaysWhatThatRowTakes` (`internal/tui3/settingunit_test.go`).
Frames: `frames/set-edit-perday-after.120x40.txt` against `set-edit-perday.120x40.txt`.

**Row 6 — a description that is cut says so.** `settingAboutLines` replaces the silent
two-line break in `listLines`: three lines, and whatever is still over the end is marked
with the same `…` every other cut on this surface wears.
Files: `internal/tui3/settings.go`.
Test: `TestASettingsDescriptionThatIsCutSaysSo` (`internal/tui3/settingunit_test.go`).
Frames: `frames/set-spending-after.60x30.txt` and `set-spending-after.80x24.txt` — the `per
day` sentence now ends `none removes the limit.` where `set-set-spending.60x30.txt` ended
`…new work waits for midnight or` and `set-set-spending.80x24.txt` ended `…none removes`.

**Row 14 — the ssh rows moved to the tab a person would open to find them.** All four
(`ssh reuse`, `ssh heartbeat`, `ssh missed heartbeats`, `ssh traffic`) left **Session** for
**Workspace**. No stored key changed; only the tab they are drawn under. Session keeps
`memory` and `fallback models`, which is its honest size.
The audit's other half — the word `Connections` doing two jobs — is NOT fixed and cannot be
from these files: that tab builds its rows from the engine's account catalog rather than
from the registry (`connectcaps.go`), so a registry row filed there would be unreachable,
and renaming it to `Accounts` means editing `connectcaps.go`. The registry category stays
`CategoryInterface`; it is not the tab, and the panel's category map only binds Spending,
Safety and Tasks.
Files: `internal/tui3/settings.go`, `internal/tui3/chrome_test.go`,
`internal/manual/chat/commands.md`, `internal/manual/chat/staying-on-that-machine.md`.
Test: `TestTheSshRowsAreOnTheTabAboutReachingAnotherMachine`
(`internal/tui3/settingunit_test.go`).
Frames: `frames/set-workspace-after.120x40.txt` and `set-session-after.120x40.txt` against
`set-session.120x40.txt`.

**Row 1, the last of it — Settings → Spending reads the day through the one seam.**
The pulse and the spend place were made to answer from one function
(`spendDayTotal`) in an earlier wave; `readDayCost` in `settingspend.go` was still opening
`app.usageLedger` itself and summing every row in the file. Two faults in one walk: over a
`--host` window the money belongs to the FAR machine and arrives through the cache the link
keeps warm (`app.usageSince`), so the tab drew this laptop's `today` on a page about somebody
else's; and the walk counted unpriced rows the other two readings leave out, so a day with a
free-tier call on it read one way here and another two keystrokes away. `readDayCost` now
goes `app.usageSince(machineDayStart(now))` → `spendDayTotal`, and the second path is gone
rather than made to agree. `session.ReadUsage` is no longer called anywhere in `tui3` outside
that seam.
Files: `internal/tui3/settingspend.go`.
Test: `TestSettingsSpendingReadsTheDayThroughTheOneLedgerSeam`
(`internal/tui3/spendmemwords_test.go`) — a window with a local ledger of `$9.99` and a far
seam of `$1.25 + one unpriced line` reads `1.25`, and a seam that has not answered is NOT
counted as a day that cost nothing.

**Row 7 — the memory rows are on rowfit, in the surface's one separator grammar.** A row is
its IDENTITY (`▾ you · 7`, `· Ships on Fridays`) and then its facts as a ranked prefix joined
by `rowSep`, with the age still right-aligned: `▾ you · 7 · mostly facts · 2 new today` and
`· Ships on Fridays · fact · let go`. `memoryReadingLine` carries `facts []rowField` where it
carried two pre-joined strings; `memoryThree` and the two hand-built two-space glues are
deleted, and `memoryRow`/`memoryHalves` are `rowPlan.fit` with this page's own gutter. The
`width >= 80` that used to gate the help clause is gone with them — a fact drops when the
room cannot hold it, which is the law and not a tier.
Files: `internal/tui3/memoryplace.go`.
Tests: `TestTheMemoryRowsUseTheSurfacesOneSeparator` (`spendmemwords_test.go`),
`TestEveryMemoryRowFitsItsCellWidthAtEveryTier` (widened to 40 and 44 cells, and its
drop-order clause re-aimed at the width where the room actually bites).
Frames: `frames/spendmem-memory-after.{160x50,120x40,80x24,60x30}.txt` against
`spendmem-memory.*.txt` — `▾ you · 7  mostly facts` → `▾ you · 7 · mostly facts`.

**Row 8 — the window's total says which total it is.** The head line's left field leads with
the span in words: `14 days came to $2.06 · 377.1k tokens`, over `today $0.14 of $500` — so
the two money figures on the page each carry the period they are the total of, in one
grammar. It is counted in the grain's own noun (`14 days`, `4 weeks`, `6 months`,
`spendSpanWord`) and NOT in dates, which stay between the arrows where SCREEN 3d put them.
The field is fitted with the control's cells already reserved, because the head's own width is
what decides whether the arrows are drawn at all — a lead added without that reservation
would have bought the sentence at 60 columns by unbinding `shift+←/→`. It degrades
`14 days came to $2.06 · 377.1k tokens` → `14 days · $2.06 · 377.1k` → `$2.06`, and
`paintedHead` now paints the line the fitter actually chose rather than assembling a second
one.
Files: `internal/tui3/spendplace.go` (`headWords`, `headFields`, `spendSpanWord`,
`paintedHead`), `internal/tui3/place_spend.go` (both `placeWindowFits` callers),
`internal/manual/chat/models-and-cost.md`.
Tests: `TestTheSpendPageSaysWhichTotalIsWhich`,
`TestTheSpendHeadNamesTheSpanInTheGrainsOwnWord`,
`TestTheSpendHeadDoesNotTakeTheWindowKeysToSayTheSpan` (`spendmemwords_test.go`).
Frames: `frames/spendmem-spend-after.{160x50,120x40,80x24,60x30}.txt` against
`spendmem-spend.*.txt`.

**Row 9 — the chart uses the room it has and its axis is two dates.** `sparkline` takes the
width: every bucket gets the same number of cells, up to `spendSparkCells` (8), so a fortnight
is 112 cells at 160 and 56 at 60 rather than 14 everywhere. `sparkAxis` is a function of its
own, as wide as the CHART and not as the frame, and both its ends are dates — `aug 21` …
`today`, the last bucket's own label, called `today` when it is today (`spendTodayWord`, one
source). The `today $0.14` that used to sit at the right end is gone: that figure is the
pointer line's two rows above, and quoting it here was one-source-of-truth broken in the
smallest way available.
Files: `internal/tui3/spendplace.go`.
Tests: `TestTheSpendChartUsesTheRoomItHasAndItsAxisIsTwoDates` (`spendmemwords_test.go`),
`TestTheSpendSparklineKeepsEveryDayInTheWindow` (re-aimed: every bucket gets the same whole
number of cells and the chart stays inside the frame).
Frames: as row 8 — `⣦⣦⣿⣦⣷⣶⣷⣤⣶⣄⣶⣀⣦⣀` and `aug 21 … today $0.14` become a 112-cell chart under
`aug 21 … today`.

**Row 13 — the shelves heading is whole before its legend.** `memoryTypeLegend` answers
`[]rowField` ranked biggest-first (the store's kind order breaks a tie, so the same counts
draw in the same order), and the section row fits the legend into what the heading LEAVES
rather than the other way round. At 80 the heading is `shelves · biggest first` with
`fact 9 · preference 2 · decision 2 · correction 1`; at 60 it keeps three kinds where it used
to drop the legend whole; `shelves · …` cannot happen.
Files: `internal/tui3/memoryplace.go`.
Test: `TestTheShelvesHeadingIsWholeBeforeItsLegend` (`spendmemwords_test.go`).
Frames: `frames/spendmem-memory-after.80x24.txt` against `spendmem-memory.80x24.txt`.

**Row 17 — the loudest day says where its door goes, and the door works.** The bare noun
`tasks` is now `enter opens it in tasks` (`in tasks` where the frame is tighter, nothing at
all where the sentence needs the whole row — rowfit's law 1, so `rebuild-the-frame… tasks`
cannot happen). Because the row names a key it IS a stop: `spendReading.body` records the
loudest day's subject as a door, so `enter` there opens the thing exactly as it does on the
rows under `what it was for`, and the foot's `enter opens what spent it` is true standing on
it. A subject of a kind with no door says nothing (`spendDoorWord`).
Files: `internal/tui3/spendplace.go`, `internal/manual/chat/models-and-cost.md`.
Tests: `TestTheLoudestDaySaysWhereItsDoorGoes` (`spendmemwords_test.go`),
`TestTheSpendCursorStopsOnlyOnRowsThatNameSomething` (now expects the loudest day's door
beside the three subjects).
Frames: as row 8.

**And the words for a memory that is no longer held.** `let go` was a count on the head AND
the tag on a row — and worse, the tag was worn by two different states: a line somebody asked
to be forgotten, and a line the machine retired on its own because something newer
contradicted it. The head added the two together. **The choice: `let go` keeps its meaning —
you let it go — and a memory the machine retired says `replaced`**, which is the manual's own
word for it ("replaced by one that contradicts it"), and the head counts them apart:
`14 held · 3 shelves · 1 let go · 1 replaced`. One phrase, one meaning, on the head and on the
row. Zero of either still draws nothing.
Files: `internal/tui3/memoryplace.go`, `internal/manual/chat/what-i-remember.md` (the count,
the row, and the `superseded` heading now also says `replaced` so the question reaches it).
Tests: `TestAMemoryLetGoAndOneReplacedAreNotTheSameWord` (`spendmemwords_test.go`),
`TestMemoryHelpWordsSayWhatTheCountersKnow` and
`TestTheMemoryPlaceTeachesUntilThereIsEnoughToRead` (both re-aimed).
**Left for whoever holds home:** `home.go`'s `learned 2 things, let go of 1` still folds a
replaced memory into `let go`. It is a different sentence on a different place and this lane
does not hold that file.

**A wall-clock test that had already aged out.** `spendLab` in `spendplace_test.go` built its
app on the WALL clock over a fixture dated august, so on 2026-09-03 the fixture's loudest day
fell out of the fourteen-day window and `TestTheSpendPlaceDrawsTheLedgerItWalkedInOn` went red
on a date nobody chose — #526's defect, one lab further along. It pins `spendTestNow` now, the
way `placeeveryone_test.go`'s `spendPlaceLab` already did.

### not fixed, and why

- **Rows 2, 12** — the pulse's navigation-dependent reading, and `dollars` rendering a
  sub-cent as `$0.0000`. `internal/tui3/pulse.go`, `homemachine.go` and `app.go`, held by
  another lane.
- **Row 11 (still open) — the daily rail is spelled two ways, and the half that is wrong is
  not in these files.** `railFigure` is this lane's and every reading that goes through it is
  right: `of $500` on the spend place, `$500 · $0.14 today` on the tab, `$5 a firing` on the
  standing row. The top line still says `$500.00`, from `dollars(facts.ceiling)` at
  `internal/tui3/pulse.go` — which another lane is inside as this is written. **The whole fix
  is that one call becoming `railFigure(facts.ceiling)`**; the numerator stays `dollars`,
  because that half is a measurement.
- **Row 10** — closed already, as audit-help's row 2: `placeSpend.hint` names `enter`, `→` and
  the window keys.
- **Row 15** (sev: low) — `rowGutter` is `internal/tui3/rowfit.go`, not this lane's.
- **Row 16** (sev: low) — the 150 blank cells at 160 columns are `overlayLines` in
  `internal/tui3/palette.go`, not this lane's. The units from row 3 do give those rows
  something to sit on, but the measure cap is still uncapped.
