# Final review — the home rethink, before it merges to `chat-v3-task`

2026-08-26, lane `home/review-final` (worktree `~/af-home-reviewf`, cut from the `home/rethink-v0`
tip at `88fdfb42`). Read against `ARCHITECTURE.md`, `FIDELITY.md`, `ACCEPTANCE.md`, `LANES.md`,
`QA-INTERACTION.md` and the whole of `git diff 0c6bed1a..HEAD` — 191 files, +32,342/−8,699.

Nothing in the tree was changed by this lane. What it ran, and what came back:

| Check | Result |
| --- | --- |
| `go test ./internal/tui3/ ./internal/session/ ./internal/store/ ./internal/standing/ ./internal/manual/... ./cmd/aforge/` | **all green** (`EXIT=0`; tui3 323s, session 61s, cmd/aforge 44s — including `TestHarnessEntriesFromStore`, which CLAUDE.md lists as a known failure and which passes here) |
| `make size` | `bin/aforge: 50,594,057 bytes, under the SIZE-BUDGET budget of 51,071,000` — 476,943 bytes of headroom, and `SIZE-BUDGET` is unchanged across the wave |
| manual pack sync (`TestThePagesAreTheFoldersOnDisk`, `TestEveryPageReadsBackWhole`) | green — the `.pack.gz` matches the folders |
| the six ARCHITECTURE laws | all six are pinned in `internal/tui3/placelaws_test.go` and pass |
| colours authored outside `styles.go` | **none** — zero hex literals, zero `lipgloss.Color(` calls in any non-test file outside `styles.go` |
| machinery vocabulary in the new place files' string literals | **none** — one hit, and it is a quotation inside a comment |
| `$0.00` on a place | **none reachable** — `spendplace.go:160-172` filters zero-priced lines out of the reading with the emptiness law cited by name |

Three probes were written under the package, run, and deleted; each is named below the finding
it confirms.

Findings are ranked by severity. **CONFIRMED** means a probe ran or the call path was read end to
end; **PLAUSIBLE** means the reasoning holds but the triggering condition was not reproduced.

---

## 1 · A conversation whose transcript merely could not be READ is `rm -rf`'d on the next launch — CONFIRMED

`cmd/aforge/chatv3_layout.go:570` (`v3EmptySession`), `:603` (`v3ReapEmpty`), `internal/session/peek.go:73`

The QA lane flagged this and did not fix it. The exact condition, traced:

`v3EmptySession` calls `session.Peek(place.Transcript())` and treats a `false` second return as
"nobody ever said anything in here". `Peek` answers `false` for **four different facts**, and the
caller cannot tell them apart:

- the file is not there (`peek.go:74-77`, `os.Open` error);
- `os.Open` failed for any other reason — EACCES on a folder whose mode drifted, EMFILE under a
  launch that opened many sessions, EIO on a flaky mount;
- every line failed `json.Unmarshal` (`peek.go:93`) — a truncated last write, a journal from a
  schema this build does not know;
- the scanner choked on line 1 because one line exceeds the 8 MiB buffer (`peek.go:86`), so
  `summary.Asked` stays 0 and `peek.go:141` returns `false`.

`v3EmptySession` then checks only for `work/`, `trees/` and `artifacts/`. **A conversation that
only talked — no task, no worktree, no artifact — has none of those three**, which is the ordinary
shape of a research or design chat. So `v3ReapEmpty` reaches `os.RemoveAll(folder.dir)` at
`chatv3_layout.go:611` and takes the folder whole: `meta.json`, `presence.json`, the task index,
the usage rows and the transcript itself.

`internal/session/sweep.go` rule 3 refuses this exact bargain in as many words — a session whose
`meta.json` is missing or unreadable **stays**, because "hiding somebody's conversation on the
strength of a lookup file is the more expensive mistake". The launch groom applies the opposite
rule to a bigger file, and the two live in one binary.

**Failure scenario.** A chat named *Pricing Research* holds forty turns and started no task. Its
last write is interrupted by a laptop lid closing, leaving a half-written final line. Next launch:
`Peek` parses every line, hits the torn one, continues, and — because the torn line was the only
`user` message in a session resumed from a compaction — `Asked` is 0. No `work/`. The folder is
deleted. There is no message, no log line and no undo, and it happened *before* the surface drew.

There is **no test anywhere for `v3EmptySession` or `v3ReapEmpty`** (`grep` over `cmd/aforge/*_test.go`
returns nothing), so no gate would have caught the fixture that already lost four conversations
during the QA lane.

**Minimal safe rule.** Reap only what is *provably* empty, and keep everything else:

```go
// A TRANSCRIPT WE COULD NOT READ IS NOT AN EMPTY ONE. sweep.go rule 3 is the
// law and this is the same fact about a bigger file: absence is a fact, and a
// read that failed is not.
func v3EmptySession(dir string) bool {
	place := session.Place{Dir: dir, Owned: true}
	switch info, err := os.Stat(place.Transcript()); {
	case err == nil && info.Size() > 0:
		return false           // there are bytes; only the writer knows what they mean
	case err != nil && !os.IsNotExist(err):
		return false           // we could not look; looking away is the safe direction
	}
	for _, kept := range []string{place.Work(), place.Trees(), place.Artifacts()} {
		if _, err := os.Stat(kept); err == nil {
			return false
		}
	}
	return true
}
```

This still reaps every genuine empty: `openSessionFile` (`internal/session/sessionfile.go:574`)
creates the transcript with `O_CREATE` and writes nothing until the first message, so an
abandoned `/new` leaves a zero-byte file and is collected exactly as it is today.

If the owner wants to go on reaping a transcript that *exists* and holds no turn, then `Peek`
must first grow a third answer — "read cleanly, nobody spoke" versus "could not read" — and the
reaper takes only the first. That is the proper fix and it is more code; the rule above is the
minimal one, and it is the one that matches the sweep next door.

Whichever lands, it needs a test: this is the only `rm -rf` on the launch path.

---

## 2 · The verb strip acts on a row the cursor has already left — CONFIRMED

`internal/tui3/verbstrip.go:95`, with `internal/tui3/place_tasks.go:506, 508, 514, 516`

`verbstrip.go` states its own law: the verbs are captured when the strip opens "so a letter cannot
act on a row that has moved out from under it", and a key that moves off the row "CLOSES THE STRIP
AND THEN MOVES". The close list at line 95 is:

```go
case "up", "down", "pgup", "pgdown", "tab", "shift+tab":
```

The tasks place binds **four more keys to the same cursor**:

```go
place_tasks.go:506   case "up", "ctrl+p":   a.taskSheetMove(-1)
place_tasks.go:508   case "down", "ctrl+n": a.taskSheetMove(1)
place_tasks.go:514   case "home":           a.taskSheetMove(-len(a.taskSheet.stops(a)))
place_tasks.go:516   case "end":            a.taskSheetMove(len(a.taskSheet.stops(a)))
```

None of the four is in the close list. Probe (`TestReviewStripGateIsIncomplete`, deleted), driving
`stripKey` with a strip holding one verb:

```
up         took=false stripStillOpen=false
down       took=false stripStillOpen=false
pgup       took=false stripStillOpen=false
pgdown     took=false stripStillOpen=false
tab        took=false stripStillOpen=false
shift+tab  took=false stripStillOpen=false
ctrl+p     took=false stripStillOpen=true      ← falls through, strip survives
ctrl+n     took=false stripStillOpen=true      ← falls through, strip survives
right      took=false stripStillOpen=true
```

(`home`/`end` could not be driven through the test helper, whose `key()` has no case for them and
returns the zero key; the code path is identical to `ctrl+n`'s — `msg.String()` is longer than one
rune, so `stripKey` returns `(nil, false)` and `taskSheetKeyPress` moves the cursor.)

**Failure scenario.** On the tasks place, `→` over `◐ rotate the staging certificate` opens the
strip with `x stop it` captured for that node. `ctrl+n`. The cursor walks to `✓ count the tabs`;
the strip is still open, and `placeStripInline` (`verbstrip.go:141`) redraws it **under the new
row**, because it places the strip under whatever `pl.cursorRow` answers this frame. Press `x`.
The certificate rotation is stopped, and the strip a person was looking at was attached to a
finished task.

**Minimal proper fix — a law, not a longer list.** A key list is a fourth place the same fact is
written down and it will drift again the next time a place binds a second spelling of `down`.
Bind the strip to the ROW instead: record the row's identity when the strip opens — the
`placeRow.hit` under `pl.cursorRow(a, rows)`, which every place already answers — and have the
frame drop the strip on any draw where the cursor's row identity no longer equals it. The strip
then cannot survive a cursor move by any key, present or future, and `stripKey`'s explicit list
shrinks to the two keys that are about the strip itself (`esc`, `left`).

---

## 3 · The spend place walks the whole standing store on every window keystroke, N+1 times — CONFIRMED

`internal/tui3/place_spend.go:425` → `:216-268` → `internal/standing/store.go:173` → `:145`

`spendWindowKey` ends with `a.rebuildSpend()` under this comment (`place_spend.go:422`):

> THE LINES ARE ALREADY IN MEMORY, so moving the window is arithmetic and never a read. A
> fortnight back is the same cache answered a different question, **which is what lets a person
> hold the arrow down.**

That is not what the code does. `rebuildSpend` (`:206`) calls `a.spendNames(p.world)`, which calls
the standing seam **once per distinct project and once more for the workspace**:

```go
place_spend.go:255   for _, item := range a.stands.Items(project.Path) { ... }
place_spend.go:262   for _, item := range a.stands.Items(a.workspace)  { ... }
```

and each of those is `Store.ForWorkspace` (`internal/standing/store.go:173`), which is
`Store.List` (`:144`) filtered — that is **`os.ReadDir` of the standing root plus a read-and-parse
of every `*.json` under it**, discarded down to one project's worth.

Probe (`TestReviewSpendWindowKeyHitsTheStandingStore`, deleted), two projects plus a workspace,
one `shift+left`:

```
standing-store reads on ONE shift+left keystroke: 3
```

Three full directory walks and three full parses of every standing document on the machine, per
keystroke, at key-repeat rate. On a machine with five projects and twenty standing orders that is
six `ReadDir`s and 120 file reads for one arrow press. The same call also runs inside
`readSpendLines`, so it repeats **on the three-second beat**, and it grows as projects × orders —
a PERF.md violation on both counts.

`TestAPlaceNeverReadsTheDiskOnADraw` (`placelaws_test.go:254`) does not catch it: it fences
`homeFolderThere`, `a.memory`, `a.stands.Items` and `a.searchStore`, but only around `body`,
`note` and `hint`. A keystroke is not a draw, and the law's own argument — "a draw that reached a
seam would reach it on every keystroke" — applies with full force to a key that repeats.

**Minimal proper fix, in two parts:**

1. **Split the join from the arithmetic.** Build the name map where the world is read — in
   `readSpendLines`, which runs on open and on the beat — and hold it on `spendPage` as `names`.
   `rebuildSpend` then reads `p.names` and the comment at `:422` becomes true.
2. **Make the join cost one read.** `spendNames` asks the seam per project only because
   `StandingSeam.Items` takes a workspace, while `ForWorkspace` lists everything anyway. Add
   `StandingSeam.All func() []standing.Item` in `cmd/aforge/chatv3_standing.go:302` and group by
   `item.Workspace` in the reading. That is a new seam, not a new `case`, which is what
   ARCHITECTURE.md asks a feature to be.

And extend law 4's test to walk `window`, `alt`, `press`, `hover` and `wheel` under the same
panicking seams, not just `body`/`note`/`hint` — this is the hole the defect came through.

---

## 4 · `looks.json` is written through a fixed shared temp path, so two windows can rename a torn file into place — CONFIRMED by reading

`internal/session/look.go:167`

```go
final := filepath.Join(root, looksStampName)
temporary := final + ".tmp"
if os.WriteFile(temporary, append(payload, '\n'), 0o600) != nil { return }
if os.Rename(temporary, final) != nil { _ = os.Remove(temporary) }
```

`looksMu` (`look.go:107`) serializes **one process**, and the file says so and calls the
cross-process race a deliberate bargain whose cost is "one place's origin". With a *shared* temp
path the cost is larger than the bargain: two windows leaving a place at the same instant both
`os.WriteFile` the same `looks.json.tmp` — truncate, then write — and either can `Rename` a
partially-written or interleaved file onto `looks.json`. `readLookStamps` (`look.go:172`) then
fails to unmarshal and answers empty, so **every place loses its origin at once**, not one.

The rename exists precisely to stop that: "a process that died halfway through an in-place write
would cost a person every origin they had rather than one — the rename is what keeps the failure
the size of the thing that failed." A shared temp file re-opens the door the rename closed.

Multi-window is not hypothetical here: CLAUDE.md's whole "working alongside other sessions"
section assumes it, and one was running in `~/af-home` while this review was written.

**Minimal proper fix.** Give each writer a private temp file, which is what makes a rename atomic:

```go
tmp, err := os.CreateTemp(root, looksStampName+".*")
if err != nil { return }
name := tmp.Name()
if _, err := tmp.Write(append(payload, '\n')); err != nil || tmp.Close() != nil {
	_ = os.Remove(name)
	return
}
if os.Rename(name, final) != nil { _ = os.Remove(name) }
```

Last writer wins, one place's origin is lost, and the stated bargain is the one the code keeps.

---

## 5 · Three of the seven places never turn the beat, so their rows AND the tab bar's counts freeze while they are up — CONFIRMED

`internal/tui3/pages.go:207`, `internal/tui3/placecounts.go:130-149`, `internal/tui3/place_standing.go:719`

`placeBase.tick` answers `false` (`pages.go:207`). `placeStanding` and `placeSettings` do not
override it; `placeTasks.tick` (`place_tasks.go:1100`) overrides it and deliberately answers
`false` too. And `placeBeat` gives up the moment a `tick` answers false:

```go
placecounts.go:143   if !pl.tick(a, now) { return nil }
placecounts.go:147   a.refreshPlaceCounts(now)
```

Probe (`TestReviewStandingPlaceNeverTicks`, deleted): `placeFor(pageStanding).tick(...)` returns
`false`. Only `place_memory.go:469`, `place_spend.go:102` and `place_search.go:77` call
`armPlaceClock`.

Two consequences, and the second is the one nobody would look for:

- **The standing place never refreshes while somebody is standing on it.** Its reading is taken in
  `standingPlaceReading` (`place_standing.go:719`) on open, and rebuilt only after one of its own
  writes (`:321`, `:3969`). An order that fires, one created in the next terminal, one paused
  elsewhere, one whose `NeedsPerson` was just set — none of them appears. That is the one place on
  the surface whose entire subject is *what happens while nobody is looking*. (Relative ages do
  update: `standingLines` is handed `a.now()` at draw time. It is the data that is frozen.)
- **The tab bar's numbers freeze with it.** `refreshPlaceCounts` sits *after* the `tick` gate, so
  standing up on tasks, standing or settings stops every tab's count from being recomputed —
  including counts about the other six places. Home does refresh them, on its own clock
  (`app.go:2754`), which makes the behaviour depend on which room you happen to be standing in.

The design says the opposite in one line (ARCHITECTURE.md's interface): "`tick(a *app, now
time.Time)` — home's 3-second beat: refresh the cached reading."

**Minimal proper fix.**

- `placeStanding.tick` re-takes the reading — `readStandingElsewhere` then
  `standingPageRows(p.win)`, settling the cursor as `window` already does at `:321-323` — and
  answers `true`; `placeStanding.open` returns `a.armPlaceClock()`.
- Move `a.refreshPlaceCounts(now)` **above** the `tick` gate in `placeBeat`. The counts are the
  bar's and not the place's; a room that wants no rows-refresh must not be able to silence the
  bar. Then arm the clock for every place and let `tick` decide only whether the room re-reads.

The tasks place's own reasoning for answering `false` ("a beat here would be a third reader of the
same record") survives that change untouched — it keeps answering `false` and keeps re-reading on
the keystroke, and the bar goes on counting.

---

## 6 · The manual states two refusals the code no longer has, and contradicts itself about a third — CONFIRMED

`internal/manual/chat/places.md:55`, `:67`, `:380`; `internal/manual/chat/keys.md:334`

CLAUDE.md: *"When you make something possible, hunt down the page that says it isn't."* Commit
`ce4a7cb0` — "every place opens, always — the three refusals are gone" — made three things
possible and left three sentences saying they are not. The gates all pass, because they check
only that a string is *mentioned*.

**`places.md:55-58`** — stale, and false:

> **`tab` goes past a place that has nothing to open.** Two rooms can be shut: **standing**, when
> nothing stands over you anywhere on this computer, and **memory**, when memory is turned off.
> `tab` and `shift+tab` walk on to the next room a person can actually get into, rather than
> stopping dead.

`nextPage` (`pages.go:1092`) walks the order table unconditionally. `pageReady` and `refusePage`
no longer exist in any non-test file.

**`places.md:67-70`** — stale, and false:

> A shut place asked for **by name** — `alt+3`, `alt+4`, a click on its word — still says why it
> will not open, on the hint line of the place you are left on.

There is no refusal path left in `showPage` to put anything back with, as `showPage`'s own header
(`pages.go:964-981`) now says at length.

**`places.md:380`** — false for one of the three it names, and it contradicts `places.md:63-65`
seventeen lines earlier in the same file:

> Commands behave the same way. `/history` on a machine that has run nothing, `/standing` on one
> nothing stands on, and `/memory` with no store all open their place and let it teach.

`/standing` (`app.go:5191`) and bare `/memory` (`app.go:5235`) do open unconditionally. `/history`
does **not** — `app.go:5278` calls `openTaskPage`, and the comment above it at `:5266` says so:
"It refuses on a machine that has run nothing rather than raising a page with a title and nothing
under it." `places.md:63-65` states that correctly. Both sentences ship.

**`keys.md:334`** — false, and it attributes the refusal to the wrong door:

> | `ctrl+.` | Open the tasks place (`/history`) … **Does nothing when nothing has run** |

`place_tasks.go:459-468` is explicit: "THE CHORD RAISES THE PLACE WHETHER OR NOT THERE IS ANYTHING
IN IT … A chord that answered with nothing was a chord a person could not tell they had pressed."
`tasks.md:979-995` has the correct account, so the corpus holds both.

This matters more than a documentation nit. These pages are the *only* thing the model knows about
this program, and the chat reads them to answer "what does this key do?". Right now it will tell a
person on a fresh machine that `ctrl+.` does nothing — which is the exact experience the wave was
built to end.

**Fix, in the same change as the merge:** delete `places.md:55-58` and `places.md:67-70`; narrow
`places.md:380` to `/standing` and `/memory` and point at the `/history` sentence at `:63-65`;
rewrite `keys.md:334` to say the chord always opens and that the *command* is what refuses.
Rebuild the pack with `make build`.

---

## 7 · FIDELITY item 5 is not built, and is not on the flagged-deviation list — CONFIRMED

`internal/tui3/place_memory.go:690`, `:23`; `FIDELITY.md` item 5

FIDELITY item 5, under the owner's "follow the exact design" order:

> **Memory (1f/2d):** `enter` on a LINE is `ask me about it` — it opens a conversation seeded with
> that line (the card moves behind `→`). The foot spells 1f: `enter ask me about it · e fix the
> wording · f forget it · tab next place`.

The code does the opposite. `placeMemory.enter` (`place_memory.go:690`):

```go
if scope, ok := p.shelfUnder(); ok { p.toggleShelf(scope); return nil }
memory, ok := p.choice()
...
p.expanded = memory.ID          // the CARD, in place — the thing the design moved behind `→`
```

The string `ask me about it` appears **nowhere** in `internal/tui3` or in
`internal/manual/chat/*.md`. The strip offers `e fix the wording`, `f forget it` and `u put it
back` and nothing else (`place_memory.go:717-763`).

The hint compounds it. `memoryFilterHint` (`place_memory.go:23`) is one constant for every row:

```
enter open a shelf · → verbs · ↑↓ move · type to filter · alt+s walk the shelves · esc close
```

so on a memory LINE — where `enter` opens a card, not a shelf — the foot names a key and describes
something else. `pages.go`'s own contract for `hint` is "what the row under the cursor can be asked
for."

FIDELITY's deviations list at the foot of the file carries four entries; this is not one of them,
and `ACCEPTANCE.md`'s 2d row reads **matches** while naming only the two design blocks that have no
mechanism. So the wave reads as having built item 5, and it has not.

**Fix — one of two, and the choice is the owner's:**

- **Build it.** `enter` on a line seeds a conversation with that line's text through the door
  `placeTalk` (`placekeys.go:307`) already opens; the card moves onto the strip as a fourth verb.
  Then the foot becomes 1f's sentence.
- **Flag it.** Add it to FIDELITY's deviation list with the reason, and change `ACCEPTANCE.md`'s
  2d verdict from **matches** to **matches, with item 5 deferred**.

Either way the hint must stop naming `enter open a shelf` on rows where `enter` does not open a
shelf — that is the one part of this that is a defect rather than a scope call.

---

## 8 · Dead code carrying a second spelling of the memory verbs — CONFIRMED

`internal/tui3/memoryplace.go:34`, `:413-422`

```go
type memoryVerb struct { ... }
func (r memoryReading) verbs(i int) []memoryVerb {
	...
	return []memoryVerb{{key: 'e', word: "e fix the wording"}, {key: 'f', word: "f forget it"}}
}
```

A repo-wide grep for `memoryVerb` returns four hits and all four are these lines — no caller in
any file, including tests. It is the pre-strip reading, left behind rather than removed.

It is worse than inert: it holds a **second spelling** of two facts that `verbstrip.go:238-240`
already declares once (`memoryFixWord = "fix the wording"`, `memoryForgetWord = "forget it"`), and
it spells them differently — the key letter is baked into the word here, while the live strip
pairs them separately (`verbstrip.go:203`: `pal.data(string(v.key)) + pal.dim(" "+v.word)`).
Revive it and the strip reads `e e fix the wording`. ARCHITECTURE.md: "A fact spelled twice is a
bug."

**Fix:** delete both, and the `store` import if it falls idle.

---

## 9 · Two comments assert laws this wave retired — CONFIRMED

**`internal/tui3/homemachine.go:411-413`**, on `machineAllowance`:

> IT IS ONE SETTING READ IN ONE PLACE. The ceiling is drawn on the `today` band and nowhere else
> on this screen — **never on the pulse, which says what has been spent and never a fraction**.

`pulse.go:161-164` draws exactly that fraction from exactly that field:

```go
figure := dollars(facts.spent)
if facts.ceiling > 0 { figure += pulseAllowanceGap + dollars(facts.ceiling) }
```

`pulse.go:36-49` carries a careful headstone for the retired law and says the owner overruled it
on 2026-08-25. One file over, the retired law is still asserted as current — one fact, two
comments, one of them false.

**`internal/tui3/pages.go:957-962`**, on `showPage`:

> IT IS A WRAPPER OVER THE EXCLUSION THAT ALREADY EXISTED, not a replacement for it.
> `[app.standDownFullscreen]` still closes every page that takes the frame, **every page still
> carries its own `open bool`**, and view.go's frame still asks those booleans in the same order —
> so `a.page` is a LABEL on state the booleans already carry.

None of that survives `e7036fe9`. `showPage` (`pages.go:982`) is `close(a) → a.page = id →
open(a)`, no place carries an `open bool`, and `standDownFullscreen` is now one line
(`settings.go:947`: `func (a *app) standDownFullscreen() { a.showPage(pageNone) }`).

Related, same shape: `place_tasks.go:26-28` promises the one-line aliases are "the seam the
refactor lane deletes when the interface lands". The lane landed;
`taskSheetKeysLine` (`place_tasks.go:970`) and `tasksChangedSince` (`:1074`) now have only test
callers and one doc-comment reference.

CLAUDE.md asks comments to state the why. A why that was true two commits ago is the most
expensive kind of comment there is — it is read as current and it is checked by nothing.

---

## 10 · A deleted test took an unprotected law with it — CONFIRMED

`homeattention_test.go:440` at `0c6bed1a` (`TestAWaitingConversationIsNotAlsoMoving`), now gone

65 test functions were deleted this wave and 319 added, and the great majority of the deletions
are the zones/tiers/strips machinery the switcher replaced — correctly removed. One is not a shape:

> WAITING OUTRANKS WORKING, once more: a conversation stopped on a question is in `needs you` and
> nowhere else, however much work it has out.

The law is still live in the code — `machineHands` skips `row.NeedsPerson()`
(`homemachine.go:192`) and both counters dedupe machine-wide items (`:206`, `:248`) — and it is now
**the pulse's** law, not a strip's: `2 want you · 4 moving` on the loudest line this wave added is
wrong the day a waiting conversation is counted in both clauses. Nothing pins it: there is no
`pulse_test.go`, and no test in the package references `machineWants`.

ARCHITECTURE.md's own rule for this: "A test that pins a shape the design replaced is rewritten to
pin the law it protected; it is not skipped, not loosened, not deleted without saying which law
died."

**Fix:** one test over `pulseSegments` with a fixture holding a waiting conversation that also has
a task running — asserting `1 want you` and no `moving` clause.

The other 64 deletions were spot-checked and the laws worth keeping are re-pinned: the
one-spinner law survives as `homebridge_test.go:394`
(`TestExactlyOneRowIsGivenTheSpinnerHoweverManyAreMoving`), and
`TestTheTaskPageRefusesToOpenWithNoTasksAtAll` died with the refusal it pinned, which
`QA-INTERACTION.md` and `pages.go:964-981` both name.

---

## 11 · `FlushUsage` can hold the terminal on the way out — PLAUSIBLE

`internal/session/usage_ledger.go:276`, called from `cmd/aforge/chatv3_process.go:296`

```go
done := make(chan struct{})
writer.queue <- usageWrite{done: done}     // blocking send into a 256-deep queue
<-done                                     // unbounded wait
```

`RecordUsage` twenty lines below takes the opposite bargain for the same case, with the reason
stated: "THE ENQUEUE IS NON-BLOCKING AND THE ROW IS THE THING THAT GIVES WAY. A full queue means
the writer is stuck on a disk that is not answering." `FlushUsage` blocks on both halves.

**Failure scenario.** The ledger lives on a mount that stops answering. `run` (`:214`) is parked
inside `file.Write`; `RecordUsage` drops rows into the void and returns instantly, so the surface
never notices. The person types `/quit`. `closeAll` closes every agent, reaches `FlushUsage`, and
either the send (queue full) or the wait (writer parked) never returns. The terminal does not come
back and `ctrl+c` is already gone — the surface has stood down.

Every other exit path is sound: `defer proc.closeAll()` (`chatv3.go:264`) covers every `return`
after the agent exists, and `closeAll` is idempotent under `p.closed`.

**Fix:** bound both halves. A spending record is worth less than the terminal, and this file
already says so.

```go
select {
case writer.queue <- usageWrite{done: done}:
	select {
	case <-done:
	case <-time.After(usageFlushGrace):
	}
case <-time.After(usageFlushGrace):
}
```

Verdict here is PLAUSIBLE rather than CONFIRMED: the reasoning is a straight read of the channel
discipline, but a hung filesystem was not staged.

---

## 12 · The pulse fills its cache from a draw, on the three places that have no beat — CONFIRMED by reading

`internal/tui3/homemachine.go:119-152`, `:394`, `:415`

`machineFactsAt` is a lazy TTL cache — "taken at most once per `homeEvery` **whoever asks first**"
(`homemachine.go:27`). `pulseLine` asks it on **every frame of every place**
(`pulse.go:117` → `:133`), and two of the facts it fills reach the disk:

- `machineStandingSpend` → `a.stands.Runs(day)` → `standing.Store.RunsSince`
  (`internal/standing/ledger.go:101`), which opens one ledger file per day in the span;
- `machineAllowance` → `config.DailyBudgetUSDAt` (`homemachine.go:416`), a settings file read —
  and internal/config caches nothing on purpose (`chatv3.go:1357` states that as a design choice).

On home, memory, spend and search that lands on a beat and the draw finds it warm. On **tasks,
standing and settings there is no beat at all** (finding 5), so the first paint after each
three-second boundary is the one that does the reading — a stall on a render path, once every
three seconds, forever.

It is the smaller half of finding 5 and the fix folds into it: once every place arms the clock,
`machineFactsAt` should be *filled* only by `refreshHome`/`placeBeat` and *read* by the draw,
never filled by it — the same split ARCHITECTURE.md draws between `tick` and `body`.

---

## 13 · Two smaller things — CONFIRMED

**`placeHeadRow` discards its caller's painting below the fit threshold.**
`internal/tui3/placeprose.go:242-248`: `painted` is computed at `:243-245` and then ignored at
`:248`, which returns `pal.dim(fit(head, width))`. A place that hands in a coloured head — the
argument exists for exactly that — silently loses the colour at narrow widths, so one row wears two
roles depending on the terminal. Latent: no current caller passes a non-empty `painted` where the
difference shows. Fix: `return fit(painted, width)` with the dim default already applied above.

**`refreshPlaceCounts` reads one file five times a beat.** `internal/tui3/placecounts.go:60` calls
`session.LastLookAt(root, id.word())` inside the loop, and each call is a full `os.ReadFile` +
`json.Unmarshal` of the *same* `looks.json` (`look.go:172`). Five counted places, five reads, every
three seconds. Constant rather than data-growing, so not a PERF.md breach — but `readLookStamps`
once outside the loop and five map lookups inside it is the same answer for a fifth of the
syscalls, and it removes the chance of the five reads disagreeing across a concurrent write.

---

## What this review looked for and did not find

Stated so the next lane does not re-walk it:

- **No `switch`/`case` on a page id outside the registry.** Law 1 is pinned and green, and the
  greps behind it are honest — it strips comments before matching, so the many comments telling the
  story of the retired switches do not mask a live one.
- **No colour authored outside `styles.go`.** Zero hex literals and zero `lipgloss.Color(` calls in
  any non-test file elsewhere in the package.
- **No machinery vocabulary reaching a person from the new place files.** The word sweep over
  `switcher.go`, the five `*place.go` readings, `placeprose.go`, `pages.go`, `placekeys.go`,
  `verbstrip.go` and the seven `place_*.go` returned one hit and it is a quotation inside a comment.
  The pre-existing hits elsewhere (`settings.go:413`'s `tenure after` — resident vocabulary, and
  `commands.go:66`'s `settings panel`) both predate `0c6bed1a` and are out of this wave's scope,
  though the first is worth a lane of its own under CLAUDE.md's product-wall rule.
- **No emptiness-law breach on the places.** `readSpend` (`spendplace.go:160-172`) drops
  zero-priced lines with the law cited by name, so `$0.00` cannot reach a spend row; the pulse
  omits every clause at zero and collapses the fraction where there is no ceiling
  (`pulse.go:152-166`); `dollars`' `$0.00` arm survives only for the live status line, which is the
  documented exception.
- **No nil-deref through the seams.** Every `a.stands.X` call site is nil-guarded (all eight
  checked individually), and `v3MemorySeam`/`v3SearchSeam` (`chatv3.go:1327`, `:1339`) both return
  an untyped nil rather than a typed nil in a non-nil interface — the trap is refused at the door
  and `v3Brain`'s header explains why.
- **No adapter scaffolding left standing.** `pageReady`, `refusePage` and the six `open bool`s are
  gone from the code; the surviving `open` fields belong to overlays and pickers, and the rewind
  sheet keeps its own by design. `standDownFullscreen` survives as a one-line call into
  `showPage(pageNone)` with 5 callers — a named helper rather than an adapter, and defensible,
  though it now lives in `settings.go` rather than beside the router.
- **No reading layer touching `*app`.** Law 5 parses rather than greps, which is the right call —
  a greppy version would fail on the headers that state the rule.
- **The manual pack is in sync** and all three manual gates pass.

---

## Ready to merge?

**Not yet.** The architecture is sound and the laws that matter are pinned — the `place`
interface, the registry, the six ARCHITECTURE laws, one key grammar, one window control, one
money ink. The whole suite is green and the binary is under budget with 466 KB to spare. What
stands in the way is a short list of things that are cheap to fix and expensive to ship.

### Must fix before `chat-v3-task`

| # | What | Where |
| --- | --- | --- |
| 1 | `v3ReapEmpty` deletes a conversation whose transcript could not be READ — data loss, on the launch path, with no test | `cmd/aforge/chatv3_layout.go:570` |
| 2 | The verb strip acts on a row the cursor has left (`ctrl+p`, `ctrl+n`, `home`, `end` on the tasks place) | `internal/tui3/verbstrip.go:95` |
| 3 | The spend place walks the standing store N+1 times on every window keystroke and on every beat | `internal/tui3/place_spend.go:425` |
| 4 | `looks.json`'s shared temp path lets two windows rename a torn file into place | `internal/session/look.go:167` |
| 6 | The manual states two refusals the code no longer has and contradicts itself about `/history` and `ctrl+.` — the manual law, and the gates cannot see it | `places.md:55,67,380`; `keys.md:334` |

### Should fix in the same wave

| # | What |
| --- | --- |
| 5 | Standing, tasks and settings never turn the beat, so the standing place and the tab bar's counts freeze while they are up (and #12 folds into the fix) |
| 7 | FIDELITY item 5 (`enter` on a memory line is *ask me about it*) is unbuilt and unflagged — build it or flag it, and fix the hint either way |
| 8 | Delete the dead `memoryVerb`/`memoryReading.verbs` and their second spelling of the verb words |

### Worth doing, not worth blocking on

9 (two comments asserting retired laws, and the test-only aliases `place_tasks.go` promised to
delete), 10 (re-pin the want/moving disjointness on the pulse), 11 (`FlushUsage`'s unbounded wait
on the exit path), 13 (`placeHeadRow`'s dropped painting; `refreshPlaceCounts`' five reads of one
file).

One process note, since three of the five must-fixes came through gaps in the pinned tests rather
than through the code: **extend `TestAPlaceNeverReadsTheDiskOnADraw` to walk `window`, `alt`,
`press`, `hover` and `wheel`** under the same panicking seams. Finding 3 is a keystroke and not a
draw, which is the only reason a law written against exactly that defect did not catch it.
