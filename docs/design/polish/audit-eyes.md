# The second eye — one read of the whole wave against dev

A read of `git diff origin/dev...HEAD` by a reader who did not write any of it,
asked for the things the people who wrote a fix cannot see: a test that passes
against the defect it names, a behaviour change with no test at all, a fix that
moved a defect rather than removing it, byte arithmetic where a terminal needs
display cells, and a person-facing string that says what the code does not do.

Its report is `secondeye.md`. These are its findings after I checked each one
against the source myself — the numbering here is the ledger's, and where my
reading differs from the report's, the row says so and the row is what counts.

1. `aforge run <name>` runs a FILE of that name instead of the saved program — cmd/aforge/run.go:49 — `namesAPlanFile` routes any first positional that exists on disk to the old `aforge plan run <plan.json>` door, so a program called `formatter` executes `./formatter` as a static plan whenever that file happens to be in the working directory, and which workflow runs depends on where the caller stood. THE REPORT IS WRONG THAT `--input` TRIGGERS IT: the union flag set on run.go:66 handles that case deliberately and there is a comment saying so. The collision is narrower and still real. Fix shape: a file only takes the old road if it DECODES AS A PLAN, not merely if it exists — and the registry wins a tie. — sev: high — frames: n/a (headless)
2. A limited run publishes an answer AND an error, against the envelope's own contract — cmd/aforge/exec.go:354 — `buildExecEnvelope` copies every `runErr` into `Error` unconditionally, while `Stop` is `execStop(outcome, runErr)`, which deliberately preserves `budget`, `turn-cap` and `deadline` when the executor returns both an outcome and an error. `envelope.go:249` states that `Error` is "why the run did not produce an answer" and is "empty on every run that produced one". So a budget stop that produced partial text hands a script both, and a script following the documented contract either throws the partial answer away or reports a startup failure that did not happen. Confirmed by reading both sides. Fix shape: derive the stop once and fill `Error` only when it is the error stop. — sev: high — frames: n/a (headless)
3. The rename test cannot fail for the reason it is named — cmd/aforge/vocabulary_test.go:68 — `captureNotice` discards the error the door returns, and every row asserts only that the one-line rename notice carries the new spelling. An old spelling that prints its notice and THEN fails as an unknown flag, rejects its old value, or enters the wrong door leaves this test green — which is the whole compatibility window it exists to protect. This is the fifth test in this wave that passes against the defect it names. Fix shape: return the door's error and assert the old spelling reaches the same place with the same value as the new one. — sev: high — frames: n/a
4. Tab geometry counts bytes where the terminal needs display cells — internal/tui3/settings.go:2351 — `tabChipCols` uses `len(title)` while the drawing and the hit-testing around it use `ansi.StringWidth`. Latent while every tab title is ASCII; the moment one is not, `tabWindow` hides the wrong number of tabs and a click lands on the wrong one. The tests slice the drawn bar as `[]rune`, so they cannot settle a combining mark either. Fix shape: `ansi.StringWidth`, and a test with a double-width rune and a combining sequence read by cell rather than by rune. — sev: low — frames: n/a
5. Per-command help wraps by bytes, not cells — cmd/aforge/usage.go:137 — `wrapAt` measures `len(...)` although its stated job is fitting an eighty-column terminal. Same class as row 4 and equally latent. Fix shape: the same display-width call, with wide and combining cases. — sev: low — frames: n/a

## fixed

**Row 1 — `aforge run <name>` no longer reads the working directory.**
Files: `cmd/aforge/run.go` (`namesAPlanPath`, new `looksLikeAPath`),
`internal/manual/chat/running-from-the-terminal.md` (`## The old spellings`),
`internal/manual/chat/saved-programs.md`, `docs/HEADLESS.md`,
`docs/design/polish/envelope-and-exits.md`.
The law went into the two sections that already carried the old rule rather than into
a new `## ` heading of its own: a short new section outranked half the corpus on BM25
and pulled `what is aforge exec for` and `what is aforge` onto the terminal page.
`internal/manual/chat_test.go`'s probe table named it both times.
Test: `TestWhatAforgeRunMeansIsTheSameFromEveryDirectory`, with
`TestARealPlanFileStillRunsAndStillSaysItIsPlanRunNow` beside it
(`cmd/aforge/run_road_test.go`).
The reading is the SHAPE of the argument and never `os.Stat`. A bare word is a saved
program's name; a separator anywhere in it, a leading `./`, `../` or `~`, or a file
extension on the end makes it a plan file and sends it down the retired `aforge plan
run` road. Where both readings would work the path form wins, because that is the one
the caller spelled on purpose, and the registry is never consulted — so no answer here
can depend on what a person's shell happened to be standing in. `aforge run formatter`
is the saved program in every directory, including the one with `./formatter` in it.
The decode test the first brief asked for was NOT built: whether a file parses as JSON
is exactly as accidental as whether it is there. The compatibility notice still fires
for a genuine old-spelling call, and the test runs both directions from two working
directories.

**Row 2 — a limited run publishes its answer and no `error`.**
Files: `cmd/aforge/exec.go` (`buildExecEnvelope`), `cmd/aforge/envelope.go` (the new
`envelopeIncomplete` constant), `cmd/aforge/subharness_run.go` (reaches for it),
`internal/manual/chat/running-from-the-terminal.md`, `docs/HEADLESS.md`.
Tests: `TestALimitedExecRunPublishesItsAnswerAndNoError`,
`TestARunThatCouldNotStartStillFillsTheErrorField`,
`TestOnlyTheStopThatMeansItNeverRanFillsTheErrorField`
(`cmd/aforge/envelope_limit_test.go`).
The stop is derived once and `error` follows it: only `stop: "error"` fills the field,
which is what `envelope.go:249` has said all along. The limit's own sentence is not
lost and did not get a new name invented for it — it goes to `incomplete`, which is
already what `aforge run` publishes "the reason it did not finish" under, and the field
name is now spelled once in `envelopeIncomplete` rather than twice as a literal.
The other two doors were checked and neither had the shape: `aforge do` writes `Error`
only in `failedErrand`, which sets `stop: stopError` on the same object, and `aforge
run` writes it only in `sayFailedEnvelope`, which is `stopError` by construction.
`TestOnlyTheStopThatMeansItNeverRanFillsTheErrorField` drives all three mappers so that
neither of them can grow the shape later.

**Row 3 — the rename test now asserts the thing it is named after.**
Files: `cmd/aforge/vocabulary_test.go` (`captureNotice`, new `doorParse`,
`watchParses`, `firstDifference`, `typed`), `cmd/aforge/usage.go` (the `parseWatcher`
seam).
Test: `TestAnOldSpellingReachesTheSamePlaceAndSaysWhatItIsCalledNow`.
`captureNotice` returns the door's error. Every row is now the SAME invocation spelled
twice, both spellings driven through the binary's own `os.Args` dispatch rather than by
calling the door the row believes the old word reaches, and the row asserts: the notice
is one line on stderr naming the new spelling; the new spelling prints no notice of its
own; the old spelling completed a parse at all; both entered the same door, holding the
same positionals, with every flag reading the same value; and both ended with the same
sentence at the same pre-provider wall. `parseWatcher` is a nil-by-default seam in
`parseCommandFlags`, written for the same reason `renameNotice` is a variable: a claim
about where an old spelling lands cannot be checked from outside a door that builds its
flag set privately. Nothing in the table spends money — every row stops at a missing
key, a plan with no nodes, an input that was never named, or a store that is not there.

**Row 4 — the settings tab strip is laid out in cells.**
Files: `internal/tui3/settings.go` (`tabChipCols`).
Tests: `TestATabChipIsMeasuredInTheCellsItDrawsAndNotInBytes`,
`TestTheSettingsTabStripPlacesEveryChipWhereItSaysInCells`
(`internal/tui3/settingcells_test.go`).
`ansi.StringWidth`, which is what `tabWindow`, `tabSpans` and `sheetTabBar` already
measure everything else with. The tests swap the strip's titles for one with a
double-width word and one with a combining accent, and read the drawn line BY CELL with
`ansi.Cut` at the very columns the span claims — the existing tests slice `[]rune`,
which is off by one cell per wide rune and walks straight past a combining mark.

**Row 5 — per-command help wraps by cells.**
Files: `cmd/aforge/usage.go` (`wrapAt`).
Test: `TestAFlagSentenceIsFoldedByDisplayCellsAndNotByBytes`
(`cmd/aforge/usagewidth_test.go`).
`ansi.StringWidth` on both halves of the fit. The test checks each folded line draws
within the width AND that every line but the last is within one word of full, so a fold
that measures bytes is caught wrapping short rather than only caught overflowing —
which a byte count never does on this text.

### Watched every test fail

Each fix was reverted and the test rerun; each is listed with what it printed.

- `TestWhatAforgeRunMeansIsTheSameFromEveryDirectory` — `os.Stat` put back: 7 of 12
  subtests red, the headline one among them (`a bare word is a saved program`, standing
  in the directory the file is in, `ran: the saved-program runner` inverted to the plan
  runner). `TestARealPlanFileStillRunsAndStillSaysItIsPlanRunNow` passes under both, by
  design: it is the guard that the fix did not narrow the compatibility window shut.
- `TestALimitedExecRunPublishesItsAnswerAndNoError` and
  `TestOnlyTheStopThatMeansItNeverRanFillsTheErrorField` — the unconditional copy put
  back: all three limits red with `published an answer AND an error`.
  `TestARunThatCouldNotStartStillFillsTheErrorField` passes under that revert and was
  checked against the OPPOSITE mistake instead — `error` never filled at all — where it
  goes red with `a script has nothing to read`.
- `TestAnOldSpellingReachesTheSamePlaceAndSaysWhatItIsCalledNow` — three separate
  mutations, each of which the OLD test was green against. (a) the door prints its
  notice and then refuses the old flag: red with `ended with "flag provided but not
  defined: -budget"; the current spelling ends with "OPENROUTER_API_KEY … is
  required"`. (b) `aliasValue.Set` drops the value: six rows red, each naming the flag
  and the two values, e.g. `plan run read --budget as "150000" where the new spelling
  reads "9000"`. (c) the dispatch sends `aforge show` to `runRevise`: red with `the old
  spelling parsed 1 door(s) and the new one parsed 0`.
- `TestATabChipIsMeasuredInTheCellsItDrawsAndNotInBytes` and
  `TestTheSettingsTabStripPlacesEveryChipWhereItSaysInCells` — `len(title)` put back:
  both red, the second with `the span for "café" covers columns 26-34, which draw
  "é     Wo"`.
- `TestAFlagSentenceIsFoldedByDisplayCellsAndNotByBytes` — `len` put back: the two
  non-ASCII rows red with `line 1 draws 66 of 74 cells and the next word "in" would
  still have fit`. The ASCII row passes under both, which is correct — it is the row
  that says the fix changed nothing for the sentences the binary has today.
