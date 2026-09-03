# audit-narrow — the surface at sixty columns

Sixty columns is a split pane, an ssh session from a train, a phone in landscape. It is not
a frame somebody is about to widen; it is the frame a developer is stuck with. An earlier
lane of this wave fixed the row heights and the eight-row cap, so what is below is what was
left: three things a person at that width still could not DO, and the rows that were found
beside them and left for the lanes that hold those files.

Frames are under `docs/design/polish/frames/`, prefixed `narrow-`, captured from `bin/aforge`
in a real terminal against the demo home. Each has a `.ans` twin with the colour.

---

1. At sixty columns the place bar collapses to the word `home` and nothing says the other six exist — /home/santosh/af-polish/internal/tui3/pages.go:495 (`placeTabBar`), whose ladder had exactly three rungs: every place, then the places with a count, then `barKeeps` alone — the seven words plus the padding each chip carries are 57 cells and the air between them (`tabGap`, settings.go:2339) is six more, so a sixty-column window overshoots by three and falls straight past the middle rung, because on a quiet machine no place wears a count; what is left is ` home` and 54 empty cells on the one row whose whole job is telling a person this program has rooms, while the pulse above it spends twelve cells on `thu 12:01am` — a developer who opened aforge in a split pane cannot discover six of the seven places at all, and the frame HAS the cells and spends them on the wrong row — fix shape: a rung between "all seven" and "one" that gives up the AIR rather than a word (57 ≤ 60, so every word survives the tier this is about), and under that a fill in the bar's own order ending in a dim count of what did not fit (`+2 more`), with the route (`tab`) staying on the foot where it already is — sev: high — frames: `narrow-home.60x30.txt` (line 2) → `narrow-home-after.60x30.txt` (line 2), `narrow-tasks-after.48x24.txt`, `narrow-tasks-after.56x24.txt`

2. Home's foot is sliced mid-word, so it names a key and then eats it — /home/santosh/af-polish/internal/tui3/pages.go:1607 (`placeMsgLine`, which handed the sentence to `fit`) and homephone.go:519, 786, 788, 795 — the places' hint lines were put on `hintFit` by another lane this wave, but home's foot is a different path and never got it: at sixty columns it reads `open in another window — enter again to move it here (it …`, which is `takeoverArmedWord` (takeover.go:67) cut by a character ruler that has no idea what a clause is — a developer reading it learns that `enter` moves the conversation and then loses the sentence that says what that costs, and the phone did the same to `type to search or start something new · ↑↓ pick · enter open` — fix shape: one fitter for every foot on the surface — `hintFit` keeps its ranked `·` ladder and grows a second one for a foot that is a SENTENCE rather than a key list, dropping the bracketed gloss first and the dash elaboration second, so what a narrow frame is left with is the statement; then route `placeMsgLine` and the phone's four sites through it, and pin the law structurally so a new one cannot land — sev: high — frames: `narrow-home.60x30.txt` (line 30) → `narrow-home-after.60x30.txt` (line 30), `narrow-home-after.80x24.txt` (line 24), `narrow-home-after.160x50.txt` (line 50, whole)

3. A conversation with no title draws a title-cased hex id as its name — /home/santosh/af-polish/internal/tui3/home.go:4782 (`homeName`), which passed a title and a path to `humanName` (resume.go:275) whose last rung is `titleCase(unpackName(sessionStem(File)))` — under Decision 26 a session folder is named with an id, and the row a person is most likely to be standing in is the one they just opened, so home draws `○ 927D303242f9d00e     aforge-v2 here`; at sixty columns that hex is a third of the row, in the one column that exists to let a person match names, and title-casing it is worse than drawing nothing because it reads as a name somebody chose — `homeName` never had the picker's middle rung either, because a `session.SessionRow` carries no opening line and home may not open a transcript on a draw — fix shape: home's own ladder in names.go — the title, then the folder's name ONLY where it reads as words, then the plain word this surface already uses for the fact (`new conversation`, hop.go:609); an id-shaped stem is refused by the guard `readableName` already owns — sev: med — frames: `narrow-home.60x30.txt` (line 24) → `narrow-home-after.60x30.txt` (line 24), `narrow-home-after.120x40.txt` (line 24)

4. The job page's key row is still painted through a plain `fit` and is cut mid-word — /home/santosh/af-polish/internal/tui3/jobpage.go:386 (`paintHint(fit(keys, width-2), …)`) — it is row 2's defect in another room: on a narrow frame the job page's foot ends in half a chord, and the way out is the half that goes — a developer watching a job on a split pane cannot see which key closes it — fix shape: one character — `hintFit` in place of `fit`, exactly as pages.go and homephone.go now do; the structural law `TestEveryHintIsFittedByDroppingClausesNotByCuttingCharacters` already names this file in `hintsStillCutByCharacter` and will tell whoever lands it to delete the line — sev: med — frames: none captured; `jobpage.go` is held by the jobs lane this wave and was not touched

5. The task record's key row is cut the same way — /home/santosh/af-polish/internal/tui3/taskrecord.go:455 (`paintHint(fit(taskCardKeys, width-2), …)`) — same defect, same cost, on the card a person opens to find out what a task did — fix shape: `hintFit`, and delete the file's line from `hintsStillCutByCharacter` — sev: med — frames: none captured; `taskrecord.go` is outside this lane's file list

6. The pulse line is all-or-nothing, so navigation is outranked by a clock — /home/santosh/af-polish/internal/tui3/pulse.go:129-131 (`gap := width - name - tail - 1; if gap < 1 { return name }`) — the four segments are joined into one string and then either fit whole or vanish whole, so a window one cell too narrow loses `1 want you` — the loudest thing this surface can say — together with the time, and there is no width at which the line says somebody is waiting without also saying what o'clock it is; at sixty columns the clock is on the frame in full while the seven rooms were not, which is the sentence "navigation is outranked by a clock" said as arithmetic — fix shape: `pulseSegments` (pulse.go:134) returns `rowField`s with their short spellings (`$1.85 / $500.00` → `$1.85`, `thu 12:01am` → `12:01am` → nothing) and the line draws them through `rowTail` at `width - name - 1`, which is rowfit.go law 3 and the same fix row 2 applies to a sentence — sev: med — frames: `narrow-home-after.60x30.txt` (line 1, 42 of 60 cells) — NOT FIXED: `pulse.go` is held by another lane this wave

7. Five more person-facing foot lines are cut by character rather than by clause — /home/santosh/af-polish/internal/tui3/place_settings.go:148 (`a.sheet.footNote()`), place_memory.go:846 (`a.mem.footer`), chords.go:320 (`chordOptionWords`), input.go:1412 and roomorch.go:1857 — none of these goes through `paintHint`, so the structural law in row 2 does not reach them, and each is a `·`-joined sentence handed to `fit`; the cost is smaller than rows 4 and 5 because none of them is the only way out of its screen, but each loses its last clause to an ellipsis on a narrow frame — fix shape: `hintFit` at each site, and then widen the structural law from "the argument to `paintHint`" to "the argument to any painter of a foot", which needs those five landed first or the law fails on the day it is written — sev: low — frames: none captured; all five files are outside this lane's file list

8. The resting foot promises `tab next place` on a frame that used to draw one place — /home/santosh/af-polish/internal/tui3/home.go:256 (`homeRestHint`) — audit-home row 17, re-read after row 1 above: at sixty columns the bar now carries all seven words, so the clause names rooms that are on the screen, and under sixty home is the phone tier, which draws its own head (`home … esc close`, homephone.go:563) and not the place bar at all — there is no width left at which the foot names places the frame is hiding, so the row costs a developer nothing now and is recorded closed rather than fixed — sev: low — frames: `narrow-home-after.60x30.txt` (lines 2 and 30), `narrow-home-after.44x24.txt` (line 1, the phone head)

---

## fixed

Rows 1, 2 and 3. Every one is pinned by a named test in `internal/tui3`, run at 60, 80, 120
and 160 columns, and verified on a frame captured from the real binary against the demo
home.

**Row 1 — the narrow bar keeps every word, and says how many it could not.**
`placeTabBar`'s ladder gained a middle rung that redraws the SAME seven words with the air
between the chips given up, which is three cells short of sixty and so covers the whole
narrow tier; under that, `barWordsAt` keeps the place you are standing in, the word under
the cursor and any place wearing a count, fills the rest in the bar's own order and stops at
the first word that will not fit (rowfit.go law 3, said about words), and `barMoreWord` ends
the row with `+2 more` — or `+2` where there are not cells for the longer spelling. The
count is a SIGN and not a door: it opens nothing and claims no span, exactly as the machine's
name at the other end of the row does, because it stands for several places at once. The
route stays where it already was — `tab next place`, on the foot of every place, which row 2
now protects to the last cell. `tabBarAt` took a `gap` and an `elided`; `barChipWord` was
factored out of it so the ladder measures a chip with the same function that paints one.
Files: `internal/tui3/pages.go`; manual: `places.md` (*Why the tab bar looks squashed on a
narrow terminal*). Tests: `TestTheNarrowBarStillSaysWhereElseYouCanGo`,
`TestABarTooNarrowForEveryWordSaysHowManyItDropped` (`internal/tui3/narrow_test.go`).
Frames: `narrow-home.60x30.txt` (line 2, ` home`) → `narrow-home-after.60x30.txt` (line 2,
`  home  tasks  standing  memory  spend  search  settings`); the rung under it on
`narrow-tasks-after.48x24.txt` (`  home  tasks  standing  memory  spend  +2 more`) and
`narrow-tasks-after.56x24.txt` (`… search  +1 more`); the wide tier unmoved on
`narrow-home-after.{80x24,120x40,160x50}.txt`, which still carry the air.

**Row 2 — hints drop whole hints, and a foot that is a sentence drops whole clauses.**
`hintFit` keeps its ranked `·` ladder and grows a second one under it for the foot that has
no `·` in it at all: `hintDropClause` takes the last whole clause off a sentence — the
bracketed gloss first, because it explains a clause that is still on the line, then the
elaboration hanging off ` — ` — and never returns half a bracket or half a word. Where the
line carries `tab next place`, a clause whose going would take it is not a clause the ladder
may drop. `placeMsgLine` and the phone's four foot sites (`homephone.go` 519, 786, 788, 795)
now go through it. The law is pinned structurally: `paintHint` may not be handed a bare
`fit` call, read off the tree with `go/parser`, with a ledger of the two files this lane may
not edit (`hintsStillCutByCharacter`) that fails BOTH when a new offender lands and when a
named one is fixed and its line left behind. Files: `internal/tui3/pages.go`,
`internal/tui3/homephone.go`; manual: `places.md` (*Why does the line at the bottom say
fewer things on a narrow window*). Tests:
`TestTheNarrowFootDropsWholeHintsAndNeverSlicesOne`,
`TestEveryHintIsFittedByDroppingClausesNotByCuttingCharacters`
(`internal/tui3/narrow_test.go`). Frames: `narrow-home.60x30.txt` (line 30,
`open in another window — enter again to move it here (it …`) →
`narrow-home-after.60x30.txt` (line 30, `open in another window — enter again to move it
here`), with the whole sentence still on `narrow-home-after.160x50.txt` (line 50).

**Row 3 — a conversation nobody has named is called `new conversation`.**
`listName` in names.go is home's own ladder: the title it settled on, then the folder's name
only where `idShaped` says it reads as words, then the word this surface already uses for the
fact. `idShaped` is `readableName`'s own guard asked as a question rather than applied as a
rewrite, so the two can never come to disagree about whether a string is a name or an id.
`homeName` is one call to it, and every list that draws a conversation — home, the card, the
phone's inbox, the switcher, home's own filter — goes through that one door. The person's own
words were considered first and are not available at this layer: a `session.SessionRow`
carries no opening line, and home may not open a transcript on a draw. Files:
`internal/tui3/names.go`, `internal/tui3/home.go`; manual: `home.md` (*What is new
conversation on my home list*). Test: `TestAConversationWithNoTitleIsNamedInWordsNotHex`
(`internal/tui3/narrow_test.go`). Frames: `narrow-home.60x30.txt` (line 24,
`○ 927D303242f9d00e     aforge-v2 here`) → `narrow-home-after.60x30.txt` (line 24,
`○ new conversation     aforge-v2 here`), `narrow-home-after.120x40.txt` (line 24).

**Row 6 — the top line gives way one segment at a time, and the clock goes first.**
`pulseLine` was all-or-nothing: `gap := width - name - tail - 1; if gap < 1 { return name }`,
so a window one cell too narrow lost `1 want you` — the loudest thing this surface can say —
together with the time, and there was no width at which the line said somebody was waiting
without also saying what o'clock it was. It walks a ladder now (`pulseRungs`), widest rung
first, and what a narrow frame shows is a SUBSET of what a wide one shows:

```
 aforge   12 want you · 4 moving · $123.45 / $500.00 · thu 1:11pm      (160, 120, 80)
 aforge   12 want you · 4 moving · $123.45 / $500.00                   (60)
 aforge   12 want you · 4 moving · $123.45                             (50)
 aforge   12 want you · 4 moving                                       (40)
 aforge   12 want you                                                  (30)
 aforge                                                                (16)
```

THE RANKING IS `clock → allowance → spend → moving → want you`, and the argument for it is
one sentence: **the terminal's own bar, the window and the wall clock all say what time it
is, and nothing anywhere else on this machine says that twelve things have stopped and will
not move until somebody looks** — so a cell that could carry either carries the one that is
only available here. Within that, `want you` outranks `moving` because a stopped thing needs
a person and a moving one does not (which is the switcher's own sort order said again), and
the spend outranks the allowance because a figure is a fact and a fraction is that fact plus
a bound, so the bound is the half that can go while the clause still says something true.

The two constraints held: **the emptiness law's one sanctioned `$0.00` is the live status
line of a conversation and this line never borrows it** — the allowance is given up by
RESPELLING the money clause (`$123.45 / $500.00` → `$123.45`, which is what this line said
for four waves) and the spend is given up by dropping the clause whole, which leaves no `$`
on the line at all. `TestANarrowTopLineNeverSaysTheDayCostNothing` walks every width from
12 to 160 and fails on a `$0.00`, on an allowance with no spend in front of it, on a `/`
with no bound behind it, and on any `…`. `pulseSegments` and `pulseRungs` are now built from
one function (`pulseParts`) so the widest line and the ladder's top rung cannot drift.
The file's old law "THE CLOCK ALWAYS DRAWS" was two laws written as one — never empty, and
never dropped — and only the first survives; the paragraph says so rather than quietly
changing. Files: `internal/tui3/pulse.go`; manual: `screen.md` (*What the top line of home
drops when it is narrow — the clock goes first*). Tests:
`TestTheTopLineGivesUpTheClockBeforeTheWorkCount`,
`TestANarrowTopLineNeverSaysTheDayCostNothing` (`internal/tui3/narrow_test.go`).
Frames: `hints-pulse-after.{60x30,80x24,120x40,160x50}.txt` (line 1) — and an honest note
about them: the demo home's machine is quiet, so its whole tail is 43 of 60 cells and the
ladder does not fire on any frame at or above sixty (below sixty home is the phone tier and
draws no pulse at all). The rungs above are the test's own table, asserted string for
string; the frames prove the wide tiers are unmoved and that the money and the clock still
read as they did.

**Row 7 — the four foot lines this lane holds drop clauses, and notes drop from the other
end.** `place_settings.go` (all four of its note lines), `chords.go:320`, `input.go:1412`
and `roomorch.go:1857` are off `fit`. Three things were learned doing it, and each is now a
law in the code:

- **A NOTE IS NOT A KEY SHEET, AND THE TWO ARE FITTED FROM OPPOSITE ENDS.** `hintFit`
  protects the LAST clause because on a key sheet that is the way out. A note is a sentence
  whose FIRST clause is the answer: settings says `saved to your profile · a project's own
  .aforge-v3/config.json is a hand edit`, and `hintFit` would have kept the aside about a
  file most people never open and dropped the answer. So `noteFit` is `hintFit`'s twin with
  the other end protected — it drops trailing `·` clauses, then falls through to the same
  `hintDropClause` sentence ladder — and the settings note and the macOS chord note go
  through it.
- **A CLAUSE THAT BEGINS `or` IS AN ALTERNATIVE AND NOT A WAY OUT.** The gate card's
  gestures are `↑ ↓ pick · enter answers · or keep typing to steer the planner`, whose last
  clause is not an escape hatch — it offers a second route to what `enter answers` already
  offers a first route to. `hintFit` drops it first now, so a narrow gate card keeps the
  pair it exists to be answered with. Without that rule the fitter would have kept the
  alternative and dropped `enter answers`, which is worse than the character cut it replaced.
- **A FILTER BOX'S PLACEHOLDER IS A KEY SHEET.** An overlay explains itself in the box a
  person is already looking at instead of spending a row on a legend, so `draftBlock`'s
  placeholder is the foot for those screens and is fitted like one.

Before and after, computed at the widths each line bites at:

```
settings foot, 60  saved to your profile · a project's own .aforge-v3/config…
                   saved to your profile
chord note,    60  your terminal sends ⌥ as a letter — turn on "use option a…
                   your terminal sends ⌥ as a letter
gate card,     60  ↑ ↓ pick · enter answers · or keep typing to steer the pl…
                   ↑ ↓ pick · enter answers
files box,     48  filter · enter open · ctrl+r reveal · ctrl+y c…
                   filter · enter open · ctrl+r reveal · esc
```

Files: `internal/tui3/place_settings.go`, `chords.go`, `input.go`, `roomorch.go`,
`pages.go` (`noteFit`, and the `or` rule inside `hintFit`); manual: `places.md` (*Why does
the line at the bottom say fewer things on a narrow window* gains the `or` rule, and a new
*Why the note above the composer drops its end while the key line drops its middle*).
Tests: `TestTheSettingsFootDropsWholeClausesNotCharacters`,
`TestTheChordNoteDropsItsRemedyWholeRatherThanSlicingIt`,
`TestAGateCardKeepsTheKeyThatAnswersItAtEveryWidth`,
`TestAFilterBoxPlaceholderDropsWholeKeysNotCharacters` (`internal/tui3/narrow_test.go`),
all four run at 60, 80, 120 and 160. Frames:
`hints-settings-after.60x30.txt` (line 28, ` saved to your profile`) against
`hints-settings-after.160x50.txt` (line 48, the whole sentence);
`hints-home-after.{60x30,80x24,120x40,160x50}.txt` for the foot that row 2 already fixed,
unmoved by this change.

**The fifth site of row 7 is not this lane's.** `place_memory.go:846`
(`fit(a.mem.footer, width-2)`) still cuts by character; `place_memory.go` is held by another
lane of this wave. Whoever takes it should pick the fitter by which END of the line carries
the answer: that footer reads `forgot 'the pricing note' · → put it back`, and the clause
worth keeping is the undo at the END, so it wants `hintFit` — the key-sheet fitter — and
NOT `noteFit`, however much it looks like a note. One character, and the pair above says
how to choose.

**Row 4 and row 5 — the job page's and the record card's key rows drop whole clauses.**
Closed by the JOBS lane, not by this one, and recorded here because that lane wrote the
change down without the numbers these rows carry. `jobpage.go:391` and
`taskrecord.go:464` both paint through `hintFit` now, and the way out moved to the END of
both feet, because that fitter keeps its last clause to the last cell there is — `esc
back` at the head of a foot was the way out being spent FIRST. The ledger row 4 named,
`hintsStillCutByCharacter` in `internal/tui3/narrow_test.go`, is EMPTY, so the structural
law reaches both files with nothing excused; its second half fails if a fixed file is left
named in it. Files: `internal/tui3/jobpage.go`, `internal/tui3/taskrecord.go`,
`internal/tui3/narrow_test.go`. Tests:
`TestTheJobPageAndTheRecordCardKeepTheWayOutOnANarrowFoot`,
`TestARunningJobsFootKeepsStopLongestOfItsFourKeys` (`internal/tui3/hintwhole_test.go`).
Frames: `jobpage-hint-after.60x30.txt` (line 30, `↑↓ scroll · c copy path · esc back`) and
`jobpage-hint-after.120x40.txt` (all four keys); the card re-captured this pass from the
current binary — `keep-taskpage.60x30.txt` (line 30,
`↑↓ scroll · m puts it in your message · esc back`, whole, way out last) beside
`taskcard-hint-after.40x24.txt` (the phone lane's bar).

**Row 8 — the resting foot names places that are on the frame.**
Recorded closed rather than fixed, which is what this row's own text asked for. Frames
captured this pass from `bin/aforge`: `keep-home.60x30.txt` — line 2
`  home  tasks  standing  memory  spend  search  settings`, line 30
`type to search or start something new · tab next place`, whole — against
`narrow-home.60x30.txt` (line 2, ` home`, under the same foot); and `keep-home.50x24.txt`
and `keep-home.44x24.txt`, where the phone tier draws its own head (` home … esc close`)
and a foot of ` enter open`, so the clause is not on the frame at all. This is `H17` in
audit-home, closed there in the same terms.

### what other lanes should know

- **`tabBarAt` takes two more arguments** — the air between chips, and how many places are
  not on the row. There are no other callers; settings' inner strip is `sheetTabBar` and was
  not touched.
- **`hintFit` now drops sentence clauses as well as `·` clauses.** A foot that ends in a
  bracket or hangs a clause off ` — ` will lose that clause on a narrow frame before it
  loses a character. Anything asserting the old character-cut ending will name you.
- **The structural law's ledger only shrinks.** `hintsStillCutByCharacter` in
  `internal/tui3/narrow_test.go` names `jobpage.go` and `taskrecord.go`. Fixing either one
  fails the test until its line is deleted in the same change — that is deliberate.
- **Rows 6 and 7 have since been taken**, except `place_memory.go:846`, which is still on
  `fit` and is held elsewhere in this wave. See the two entries above for what changed and,
  for that one site, which of the two fitters it wants.
- **There are TWO fitters now, and picking the wrong one is worse than the character cut.**
  `hintFit` protects the END of the line (a key sheet's way out); `noteFit` protects the
  FRONT (a statement's answer). Ask which end of your line a person is reading for. A
  trailing clause that begins `or` is treated as an alternative by `hintFit` and is the
  first thing off the row.
- **`draftBlock`'s placeholder now goes through `hintFit`.** Every overlay whose box
  explains itself — the deliverables filter, the connections filter, the subharness list,
  resume, the memory editor — drops a whole key on a narrow frame instead of half of one.
  Anything asserting the old `…` ending will name you.
- **The pulse is no longer one string.** `pulseSegments` still answers the widest line, but
  it and `pulseRungs` are both built from `pulseParts`; a new segment goes in `pulseParts`
  and gets a place in the ladder's rung list, or it will never be dropped.
- **`hintsStillCutByCharacter` was untouched by this lane and is now EMPTY** — the jobs and
  task lanes landed `jobpage.go` and `taskrecord.go` and deleted their lines. None of this
  lane's four sites went through `paintHint`, which is why the structural law never reached
  them; widening that law from "the argument to `paintHint`" to "the argument to any painter
  of a foot" needs `place_memory.go:846` landed first, or it fails on the day it is written.
