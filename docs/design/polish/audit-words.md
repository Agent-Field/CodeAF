# The words — one read of every person-facing string the wave adds

Twelve lanes wrote these strings and none of them read the others, so this pass
asks one question only: does the wave SAY ONE THING ONE WAY? It reads the
person-facing literals in `git diff origin/dev...HEAD` — not comments, not
fixtures, not log lines — against five rules: one idea one spelling, no
machinery vocabulary, no sentence untrue of the code around it, the emptiness
law, and verbs first.

The raw report is `wordcheck.md`. Every count below was counted on `origin/dev`
rather than guessed, and where I overruled the report the row says so and my
ruling is what counts.

1. The help sheet gives the same escape four names — internal/tui3/commands.go:991 `esc comes back`, :1042 `esc goes back`, :1031 `esc leaves`, against `esc back` on the cards — at exactly the moment a person opens `?` to learn the controls, so four phrasings read as four different gestures. Counted on dev: `esc back` 36, `esc leaves` 32, `esc goes back` 4, `esc comes back` 3. Fix shape: `esc back` everywhere; it is the largest exact spelling and the cards, pickers, rewind and hop already share it. — sev: med — frames: help sheet
2. A run that started, spent money and failed tells a script it never ran — cmd/aforge/envelope.go:120 publishes exit 1 as `it could not be run at all — no key, bad arguments, the store would not open`, but `execStop` maps `exec.StopError` — which is an OUTCOME, from a run that started — onto `stopError`, and `exitFor` sends that to exit 1. CONFIRMED AGAINST HEAD after the envelope batch landed; that batch fixed which field carries the sentence, not which rung carries the run. Fix shape: an outcome that exists means it ran, so `exec.StopError` is the `ran and did not finish` rung; exit 1 is reserved for `outcome == nil` and refusals before the work starts. The legacy escape hatch keeps the old numbers for anyone pinned to them. — sev: high — frames: n/a (headless)
3. Two of the first actions on the help sheet are labels, not instructions — internal/tui3/commands.go:1052 `ctrl+,         settings` and :1054's `not here`, among neighbours that say open, switch, copy, delete, pause, stop. A person has to infer what the key does. Fix shape: `open settings`, and `keep it out of here`. — sev: low — frames: help sheet
4. The terminal reader hands a person `node` and `harness` — cmd/aforge/why.go:26 `usage: aforge why self|<node-id>`, :174 `the harness`, cmd/aforge/rebuild.go:90 `rebuilt %d nodes from %d journaled events`, repeated through the manual's terminal page. Both words are banned by this repository's own rules, and `the harness` is the worst of them because it names neither who said the note nor what happened. MY RULING, AND IT SPLITS THE ROW: the PROSE is ours and changes now — `the harness` becomes `aforge`, the receipt counts `steps`. The `<node-id>` ARGUMENT SPELLING IS NOT OURS TO RENAME in a polish wave: `aforge why` and `aforge rebuild` belong to the debug-record epic and a public argument name is a compatibility surface. That half is reported, not fixed. — sev: med — frames: n/a
5. The `wake` receipt asserts every empty count — cmd/aforge/wake.go:213 always prints `examined %d, checked %d, fired %d, no %d, errors %d, rail waits %d, practice %d, learning %d`, so the manual's own example reads `no 0, errors 0, rail waits 0, practice 0`. Four figures claiming a measurement where nothing happened, slower to scan than the truth. This is not the live-status exception, which exists only so a status segment does not jump sideways. Fix shape: build the line from the clauses that have something in them, and say plainly that nothing happened when none do. — sev: med — frames: n/a
6. A fold on the narrow place bar loses the fold mark every other fold carries — internal/tui3/pages.go:524 writes `+N more` and `+N` while the command menu's fold at commands.go:831 goes through `foldLine` and yields `▸ N more`. Both mean the same thing; only one is marked, so a person cannot tell whether `+3 more` is a count, a door or a fold. Counted on dev: `foldLine(` 11 call sites, the `▸ ` glyph 165 occurrences. Fix shape: the bar's remainder through `foldLine`, and a width fallback that gives up the word `more` before it gives up `▸`. — sev: med — frames: narrow tier
7. The place map names a category where its neighbours name an action — internal/tui3/pages.go:1385 `→ verbs on this row`, beside `alt+1…7 go`, `alt+enter send`, `esc close`. Fix shape: `→ show actions for this row`. — sev: low — frames: map
8. The manual says a saved program `reports 0` for facts it does not measure — internal/manual/chat/running-from-the-terminal.md:97, about `steps`, and the same is true of spend, tokens and seconds on a run that never started. MY RULING, AGAINST THE REPORT, WHICH WANTED THE KEYS DROPPED: A SCRIPT CONTRACT WINS OVER THE EMPTINESS LAW HERE. That law governs what a PERSON reads on a screen; `--json` is read by a program, and a key that vanishes when a number is unknown breaks every caller that reaches for it — which is the opposite of the guarantee `COMMANDS.md` makes. The keys stay. What is actually wrong is the SENTENCE: `reports 0` tells a reader a measurement was taken. Fix shape: the manual says the program does not measure it, and the envelope's own field comments say the same. — sev: low — frames: n/a

---

## fixed

**Row 6 — the narrow place bar's remainder wears the fold mark.**
IT IS THE SAME DEFECT AS audit-home's ROW 14, one surface over, and it uses the
same source of truth. `placeprose.go` grew `foldSpellings(n, clause)` — the fold
line at every length it will give way through, widest first, WITH THE MARK ON
EVERY RUNG — and `barMoreWord` walks it, so the bar says `▸ 3 more` and then
`▸ 3` where it used to say `+3 more` and `+3`. The word `more` gives way before
`▸` does, because `▸` is the half that says a list has more behind it; a ladder
that dropped the glyph first left `+3`, which cannot be told from a count or a
badge.
Files: `internal/tui3/pages.go`, `internal/tui3/placeprose.go`;
manual: `places.md`; `internal/tui3/narrow_test.go` moved onto the marked count.
Test: `TestTheBarsRemainderWearsTheSameFoldMarkTheMenusDoes`
(`internal/tui3/barfold_test.go`). Reverted: it prints
`with 10 cells of room the bar drew "+3 more", want "▸ 3 more"`.
Frames: `home2-bar.56x24.txt` (`… spend  search  ▸ 1`) and
`home2-bar.48x24.txt` (`… memory  spend  ▸ 2`), captured this pass from
`bin/aforge`.

**Row 7 — the map says what the arrow does.**
`placeMapVerbWords` is `→ show what this row can do`. It read `→ verbs on this
row`, which named a category on a line where `alt+1…7 go to a place`,
`alt+enter send it off as a task` and `esc close` all name an act. The card's own
`→ verbs: pause, stop` keeps the noun and is left alone: there the acts are
listed immediately after it, which is what this line has no room to do.
Files: `internal/tui3/pages.go`; manual: `places.md`;
`internal/tui3/pages_test.go` now reads the constant rather than a fragment of
the old spelling.
Test: `TestTheMapNamesWhatTheArrowDoesAndNotWhatItIsCalled`
(`internal/tui3/barfold_test.go`) — it holds the whole line to `key + verb` on
every clause, not just this one. Reverted: it prints
`the map's arrow clause reads "→ verbs on this row", which names the machinery's
category rather than the act`.

## fixed

**Row 2 — a run that started, spent money and failed no longer tells a script it never
ran.** `cmd/aforge/exec.go`, `cmd/aforge/envelope.go`,
`internal/manual/chat/running-from-the-terminal.md`. Tests
`TestARunThatSpentMoneyAndFailedDoesNotTellAScriptItNeverRan`,
`TestOnlyARunWithNoOutcomeAtAllCouldNotBeRunAtAll`,
`TestTheLegacyExitCodesAreUntouchedByTheRungThatMoved`
(`cmd/aforge/exitrung_test.go`) and
`TestTheTerminalPageSaysARunThatStartedNeverLeavesOnExitOne`
(`internal/manual/terminalpagetruth_test.go`).

`execStop` now returns `stopError` — the reason that lands on exit 1 — for a nil outcome
and for nothing else. `exec.StopError` is an outcome, so it is `stopIncomplete`: it ran and
part of the work does not stand. A `StopDone` outcome handed back with an error beside it
goes the same way. `runExec` no longer substitutes an `exec.Outcome{Stop: exec.StopError}`
for a nil one — that substitution erased the single fact exit 1 is about — and
`buildExecEnvelope` reads the stop before it invents an empty outcome. The one-line answer
printed to a person is now the envelope's own `answer`, so stdout and `--json` cannot say
two things. The ladder table itself is UNCHANGED: `exitCannotRun` still claims `stopError`
and nothing else, and its comment now states the promise the code keeps.

The legacy path holds. `execExit` still short-circuits into `execLegacyExitCode` before the
ladder is consulted, so `AFORGE_EXIT_CODES=legacy` returns exactly 0/2/3/4/5/6 as before —
including 5 for a mid-run provider failure, which is the rung that moved on the new ladder.
It grew one guard, `outcome == nil` → 5, because the nil case can now reach it.
`TestTheLegacyExitCodesAreUntouchedByTheRungThatMoved` drives all eight old rows and then
re-checks the same run with the hatch unset.

Two shipped tests encoded the defect as their premise and were rewritten with it named:
`TestARunThatCouldNotStartStillFillsTheErrorField` drove `exec.Outcome{Stop: exec.StopError}`
as "a run that could not start" (it is a run that STARTED and broke) and now drives a nil
outcome; `TestExecJSONSaysWhyTheRunFailed` asserted the sentence was in `error` and now
asserts it is on the object either way — `error` for the run that never started,
`incomplete` for the run that broke. `TestTheExitLadderIsOneTable` and
`TestLegacyExitCodesRestoresExecsOldRungsAndNothingElse` each had one row moved, with the
reason written beside it.

**NOT FIXED, AND IT NEEDS A HAND: `docs/design/polish/envelope-and-exits.md` is now stale.**
Its before/after row `the provider failed, the key was missing, the model id was rejected |
5 | **1** | 5` says 1 where the ladder now says 2, and the sentence under the `stop` table —
"`exec`'s five existing values … are spelled exactly as they were" — is no longer true of
`error`, which is `incomplete` on a run that started. The file is outside this lane's
allowed set, so it is reported rather than edited.

**Row 1 — the help sheet spells the escape gesture one way.** `internal/tui3/commands.go`,
`internal/manual/chat/commands.md`. Test `TestTheKeySheetSpellsTheEscapeGestureOneWay`
(`internal/tui3/escword_test.go`). Four rows now read `esc back`: the places row's
continuation (was `esc comes back`), `ctrl+t` (was `esc leaves`), `ctrl+k` (was `esc goes
back`) and `space space` (was `esc comes back`). The test checks both the absence of each
wrong spelling, line by line, and that each of the four rows still NAMES the key — a row
that dropped the clause would otherwise pass.

**The report's 32 `esc leaves` were counted wrong, and the row is narrower than it looked.**
Of the 30 in Go source on this tree, 27 are COMMENTS — `// esc leaves everything exactly as
it was` and its kin, in input.go, room.go, settings.go, rewind.go, palette.go and eleven
test files. Only three are strings a person ever reads, and two of them are a DIFFERENT
sentence: `internal/tui/settings.go:431` `enter saves · ctrl+u clears · esc leaves it as it
was` and `internal/tui3/standing.go:186` `…(enter sends it, esc leaves it alone)`. Both are
said over a value being edited and mean *the edit is discarded*, not *you moved* — they are
not a spelling of the navigation gesture at all, and they stay. The third was
commands.go:1031, which is the row above. So the competing label was ONE string, not
thirty-two. The strongest evidence that `esc back` is right is not the count: `hop.go:1137`
spells the switcher's own strip `esc back` while the help sheet described that same
`ctrl+k` panel as `esc goes back` — one panel, two spellings, four rows apart on the one
screen.

**Row 3 — two help-sheet rows say what their key does.** `internal/tui3/commands.go`,
`internal/manual/chat/commands.md`. Test
`TestTheKeySheetSaysWhatTheSettingsAndStandingKeysDo` (`internal/tui3/escword_test.go`).
`ctrl+,         settings` is `ctrl+,         open settings`, and `p s n          in
/standing: pause one · stop it · not here` ends `· keep it out of here`. The test asserts
the new clause AND that the row's whole tail is no longer the old one, so a reword that
dropped the meaning again cannot pass.

**Row 5 — the wake receipt says only what happened.** `cmd/aforge/wake.go`,
`internal/manual/chat/running-from-the-terminal.md`. Tests
`TestTheWakeReceiptSaysOnlyWhatHappened` and
`TestAWakePassThatFoundNothingSaysSoRatherThanPrintingZeroes`
(`cmd/aforge/terminalwords_test.go`). A new `wakePassWords` builds the line from the
clauses that have something in them, so the manual's own example is now `examined 3,
checked 2, fired 1, learning 2`. **When nothing happened it says `nothing was waiting to be
looked at.`** — a sentence rather than eight zeroes, because a receipt of zeroes and a
reader that failed look identical, which is why `why self` already answers an empty day the
same way. The second test asserts the empty line contains no digit at all; the first drives
each of the eight counts alone, so a clause that stopped being printed when it DID have
something is caught too.

**Row 4 — the prose half.** `cmd/aforge/why.go`, `cmd/aforge/rebuild.go`,
`internal/manual/chat/running-from-the-terminal.md`. Tests
`TestTheTurnRecordSignsItsOwnNotesWithTheProductsName` and
`TestTheRebuildReceiptCountsStepsAndNotNodes` (`cmd/aforge/terminalwords_test.go`), plus
`TestTheTerminalPageQuotesTheWordsWhyAndRebuildActuallyPrint`
(`internal/manual/terminalpagetruth_test.go`). A note in `aforge why`'s record is signed
`turn 4 · aforge` rather than `turn 4 · the harness`, and `aforge rebuild` prints `rebuilt
128 steps from 4173 journaled events`. Both tests drive the real door — a store written and
read back through `runWhyTo` and `runRebuildWith` — rather than the string-building
function, because the defect is what a person sees after typing the command.

**NOT FIXED, BY THE RULING: the `<node-id>` argument at why.go:26.** `usage: aforge why
self|<node-id> [--db path]` still says `node`, and so does `aforge why`'s own empty-record
sentence, which names the id it was given. A public argument name is a compatibility
surface: every script, every runbook and the `--help` line that quotes it would have to move
together, and `aforge why` and `aforge rebuild` belong to the debug-record epic. What it
would need: one name chosen for the thing (`<step-id>` matches the `steps` the envelope, the
task page and now `rebuild` all count), the usage line, the manual's four `aforge why
<node-id>` spellings, `TestTheChatManualMentionsEveryVerbTheCommandLineAnswersTo`'s
neighbours in `internal/manual/terminalverbs_test.go`, and either an accepted alias for one
release or a change entry that says the old spelling is gone.

**Row 8 — the manual no longer claims a measurement that was never taken.**
`internal/manual/chat/running-from-the-terminal.md`, `cmd/aforge/envelope.go`. Test
`TestTheTerminalPageDoesNotSayASavedProgramReportsZeroSteps`
(`internal/manual/terminalpagetruth_test.go`). `A saved program does not count them and
reports 0` is now `A saved program does not measure it: the key is still there, holding
`0`, and that `0` is a measurement nobody took rather than a count of none`, and the
`Steps` field comment in envelope.go says the same. **The keys stay, per the ruling** — the
test asserts all ten contract keys are still documented on the page, so the sentence can
never be fixed by deleting the row. The `error` row moved with row 2: `Empty on every run
that started, however it ended`.

**One retrieval note for whoever writes here next.** Putting the literal `turn 4 · aforge`
into the terminal page was enough, on its own, to pull
`internal/manual/chat_test.go`'s probe *"how do I see what aforge did"* off `keys` and onto
`running-from-the-terminal` — one token, in a section whose heading already carries *see*,
*what*, *did* and *aforge*. It was fixed on the page, not in the test: the sentence
immediately under that heading repeated `aforge why` for the third time in four lines and
now reads `This prints one piece of work's whole record…`, which is better prose and puts
the probe back. Watch for it if you add another `aforge` to that section.
