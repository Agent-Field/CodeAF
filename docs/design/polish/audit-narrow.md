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
- **Rows 6 and 7 are not this lane's to take.** `pulse.go`, `place_settings.go`,
  `place_memory.go`, `chords.go`, `input.go` and `roomorch.go` are held elsewhere in this
  wave; each row above states the fix shape so whoever holds them can land it in one edit.
