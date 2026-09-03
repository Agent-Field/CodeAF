# CLI audit — everything aforge prints when it is not the full-screen TUI

Audited against `bin/aforge` at `2d4c4dde8 built 2026-09-02 22:57`, driven with
`HOME=<demo home>` from `/home/santosh/af-polish`. Every capture named below is under
`docs/design/polish/frames/`.

**What is already right, and should not be touched:** `do --json` is a real contract —
one object on stdout, valid JSON on every path including a run that never started, an
`error` field carrying the sentence, and a non-zero exit alongside it
(`cli-json-failure.txt`). Progress is line-appended to stderr with no ANSI, no carriage
returns and no spinner, so a pipe and a log file read cleanly (`cli-do-json.stderr.txt`).
`logs --json` is clean JSONL with the path line on stderr. The spend-consent refusal, the
`--timeout` parse message and the `devices revoke` miss all name the cause *and* what to
do. The usage list is grouped by what a person is trying to do, not alphabetically.

---

1. `--help` on nine subcommands prints one line — Go's internal `flag: help requested` — and no usage at all — cmd/aforge/doctor.go:66, logs.go:70, why.go:21, competence.go:22, wake.go:86, rebuild.go:30, notebook.go:27, cache.go:59 (all `SetOutput(io.Discard)` then the parse error escapes to main.go:112) — the one gesture every developer makes first returns a Go package's internal string, tells them nothing about the command, and exits 1; there is no other way to learn what `aforge why` or `aforge logs` take — each door catches `flag.ErrHelp` from `Parse`, prints its own usage block, and returns nil (exit 0) — sev: high — evidence: docs/design/polish/frames/cli-help-exitcodes.txt

2. `--help` on the seven doors that do print usage still ends with `error: flag: help requested` and exits 1 — cmd/aforge/main.go:112 (do.go:182, main.go:362, main.go:473, run.go:38, exec.go:32, chatv3_at.go:212, services.go:14) — asking for help is reported as a failure, so any script or Makefile that runs `aforge do --help` to check the binary is healthy sees a failing command, and a person reads "error" under text that is not an error — same fix: intercept `flag.ErrHelp` before the `error:` line — sev: high — evidence: docs/design/polish/frames/cli-subcommand-help.txt

3. `aforge do` prints a four-deep Go wrapped-error chain as the deliverable, on stdout — cmd/aforge/do.go:2434 (the string is built upstream in the splice/compile path) — the answer a person or a script reads is `I couldn't apply that request: splice failed: compile request: compile intent: API error (400): nosuch/model-xyz is not a valid model ID`; three internal package verbs come before the only fact that matters and nothing says what to do about it — unwrap to the terminal cause and add the action (`that model id is not one OpenRouter has — run \`aforge models\` or pass --model`) — sev: high — evidence: docs/design/polish/frames/cli-badmodel.txt

4. `exec --json` carries no reason for a failure — cmd/aforge/exec.go:22-29 (`execEnvelope` has Text/Stop/Usage/Artifacts/Turns/ElapsedMS and no error field) and exec.go:123 writes the sentence to stderr only — a caller reading exec's stdout gets `{"text":"","stop":"error",…}` and cannot learn that the model id was rejected; this is exactly the defect `do` fixed with its `Error` field (do.go:172) and exec never got — add the same `error` key — sev: high — evidence: docs/design/polish/frames/cli-json-failure.txt

5. The `aforge do` footer breaks the emptiness law twice on the last line of every headless run — cmd/aforge/do.go:2449 (`"%s · %s · $%.4f"`) — a run that spent nothing ends `0s · 0 nodes · $0.0000`, which is three claims nobody earned on the one line a person reads to find out what happened; the codebase already owns the correct helper (internal/config/settings.go:2286 `spentFigure` / :2296 "a day that has cost nothing says nothing") — drop each segment whose value is zero — sev: high — evidence: docs/design/polish/frames/cli-badmodel.txt

6. `aforge exec` has a six-value exit ladder that is documented nowhere a user can reach — cmd/aforge/exec.go:223-241 (0 done, 2 budget, 3 turn cap, 4 deadline, 5 error, 6 done-but-empty) — a harness wrapping `exec` cannot branch on it without reading the source, and `aforge --help` spells out the codes for `do` and for `run subharness` while saying nothing about exec's; a run against a bad model exits 5, which every caller will read as a crash — add the ladder to the `exec` line in `usageText` and to the flag help — sev: high — evidence: `HOME=$DEMO_HOME ./bin/aforge exec "print ok" --model nosuch/model-xyz --timeout 20` → exit 5

7. `aforge --help` hard-wraps mid-word on an 80-column terminal — cmd/aforge/main.go:221 (the longest line is 126 columns, with a 25-column hanging indent) — 80 columns is the default terminal width and the wrap breaks inside words (`resuming you|r last conversation`) while the continuation indent stops aligning, so the one page that has to be readable is the least readable thing the binary prints — re-lay the block to 80 columns — sev: med — evidence: docs/design/polish/frames/cli-help.80x200.txt and cli-help.60x200.txt

8. A misspelled subcommand suggests nothing and dumps the whole 127-line usage — cmd/aforge/main.go:212 — `aforge lgos` and `aforge doo` both answer `unknown command` followed by every command and the entire environment table, so the one line that matters scrolls off the top and the obvious next step (`logs`, `do`) is never named — print the error, the nearest match, and `run \`aforge --help\`` — sev: med — evidence: docs/design/polish/frames/cli-unknown-and-version.txt

9. A missing positional argument dumps the same 127 lines — cmd/aforge/main.go:596 and :608 (`fmt.Errorf("no goal given\n\n%s", usageText)`) — `aforge do` and `aforge plan` with nothing after them scroll the environment table past the reader for the sake of one missing quoted string — print the one-line form for that command instead — sev: med — evidence: docs/design/polish/frames/cli-errors.txt

10. Three commands parse no flags at all and read a flag as a positional — cmd/aforge/main.go:545 (`show`), manual.go, models.go — `aforge show --help` answers `error: open --help: no such file or directory`, `aforge manual --json` answers `there is no manual page named "--json"` and lists 39 pages, and `aforge models --help` silently ignores the flag and runs the command; a person probing an unfamiliar command gets a filesystem error about a flag — recognise `-h`/`--help` in each — sev: med — evidence: docs/design/polish/frames/cli-subcommand-help-2.txt, cli-errors2.txt

11. A bad flag prints its own message twice — cmd/aforge/main.go:112 on top of the flag package's own output — `aforge do "x" --nosuchflag` prints `flag provided but not defined: -nosuchflag`, then the whole flag list, then `error: flag provided but not defined: -nosuchflag` again; the reader has to work out that the two are one fact — discard the flag package's output (as doctor/logs already do) and print one line plus the command's own usage — sev: med — evidence: docs/design/polish/frames/cli-errors.txt

12. Filesystem failures reach the person as Go wrapped chains over raw syscall text — cmd/aforge/do.go (`create chat workspace: mkdir /nope: permission denied`), exec.go (`mkdir /nope: permission denied` — which does not even name the flag that caused it), notebook.go (`open notebook: stat …: no such file or directory`), why.go (`open receipts: stat …`), main.go:547 and run.go:57 (`open /nope/graph.json: no such file or directory`) — none says which flag was wrong or what to do, and `stat`/`mkdir`/`open` are the operating system's words, not aforge's — say `-w names /nope/dir, which does not exist and cannot be created here` — sev: med — evidence: docs/design/polish/frames/cli-errors2.txt, cli-errors.txt

13. The missing-key sentence is right in one command and bare in four — cmd/aforge/do.go prints `aforge do needs a model to work with. / export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.` while `exec`, `plan`, `models` and `run` print only `error: OPENROUTER_API_KEY (or OPENAI_API_KEY) is required` — the most common first-run failure tells four out of five callers the cause and not the remedy, and `do` then repeats itself in machine form on the next line — hoist the two-line sentence to the one place the key is resolved and drop the duplicate — sev: med — evidence: docs/design/polish/frames/cli-nokey.txt

14. `aforge doctor` says nothing about a missing key — cmd/aforge/doctor.go:212-217 (the block is brain / resident / standing watch / spend / standing / model calls) — doctor is the command a person runs when nothing works, and on a home with no key it reports six healthy-looking rows and exits 0 — add a key row that says whether one is configured and where it came from — sev: med — evidence: docs/design/polish/frames/cli-nokey.txt

15. `doctor` and `notebook` print money and counts that are zero — cmd/aforge/doctor.go:204 (`$%.2f today · rail $%.2f` → `$0.00 today · rail $500.00`), doctor.go:208 (`0 active charters · 0 pending questions`), notebook.go:125 (`today's spend: $0.00 of $500.00 daily rail`) — the emptiness law makes zero print as nothing outside the live status line, and these are not it; a fresh machine reads as a machine that measured zero rather than one that has not measured — use the `spentTodayReceipt` shape already in internal/config/settings.go:2296 — sev: med — evidence: docs/design/polish/frames/cli-readonly-commands.txt

16. `record kept at <path>` is printed for runs that never started — cmd/aforge/do.go:376 — it appears above `error: create chat workspace: mkdir /nope: permission denied` and above the missing-key sentence, so the person is pointed at a folder for a run that produced nothing, on the exact paths where they are already looking for the cause — hold the line until the run is admitted — sev: med — evidence: docs/design/polish/frames/cli-errors2.txt, cli-nokey.txt

17. A bad subharness name is reported as an empty input — cmd/aforge/subharness_run.go:440, reached from subharness_run.go:73 before the name is ever resolved — `aforge run subharness nosuchharness --input -` answers `the input is empty — there is nothing here for the run to do` and never mentions the name, so the reader fixes the wrong thing; the comment at :70 says the ordering is deliberate, but the name can be checked first at no cost — sev: med — evidence: docs/design/polish/frames/cli-errors2.txt

18. `--debug` is absent from `aforge --help` — cmd/aforge/main.go:221 (the flag exists on `do`, `exec`, `chat` and `resume`, and has its own manual page `internal/manual/chat/debug-record.md`) — the whole debug-record feature is invisible to anyone reading the usage text, which is the only place a headless caller looks; `--no-host` is missing the same way — add both — sev: med — evidence: `grep -c -- '--debug'` over the `usageText` literal returns 0

19. `aforge rebuild` uses machinery vocabulary and runs its prompt into its error — cmd/aforge/rebuild.go:64 (`Rebuild every materialized view in … from the event journal?`), main.go:221 (`discard every derived table and replay the journal`), rebuild.go:66-70 — "materialized view", "derived table" and "event journal" are the storage layer's words in a sentence a person has to answer; and with stdin closed the un-terminated `[y/N] ` prompt and `error: rebuild cancelled` print on one line, with declining reported as an error and a non-zero exit — say what is discarded in plain words, terminate the prompt, and treat "no" as a normal exit — sev: med — evidence: docs/design/polish/frames/cli-prompts.txt

20. `logs --tail notanumber` answers `parse error` — cmd/aforge/logs.go:70-80 (plain `flags.Int`) — Go's default message names nothing the person can act on, while `do --timeout` in the same binary answers `a duration such as 15m or 2h, or a number of seconds`; `exec --turns` has the same hole — give the numeric flags a `flag.Value` with a sentence, as `wallFlag` already does — sev: med — evidence: docs/design/polish/frames/cli-errors2.txt

21. Two commands print a column header over no rows — cmd/aforge/why.go:62 (`TRIED\tCOST\tLEARNED`) and notebook.go:99 (`SEQ SCOPE KIND AGE USES RIDES BAD STATUS BELIEF`) — on a fresh machine the header is the entire output, which reads as a table that failed to load rather than as nothing to show; `competence` gets this right with `No competence evidence yet.` — draw the header only when there is a row — sev: low — evidence: docs/design/polish/frames/cli-readonly-commands.txt

22. `aforge services` prints absolutely nothing, and its rows are raw tab-separated fields — cmd/aforge/services.go:29-39 — with no services it exits 0 having written zero bytes, which is indistinguishable from a command that did not run; with services it emits `name\tstatus\tage\thealth\tlogpath` with no header and no alignment, unlike every neighbouring command — say the one sentence, and align the rows the way `doctor` does — sev: low — evidence: docs/design/polish/frames/cli-readonly-commands.txt

23. The two help surfaces spell the same flags differently — cmd/aforge/main.go:221 writes `--json`, `--timeout`, `-w`, while every per-command help built by the flag package writes `-json`, `-timeout`, `-w` — a reader comparing `aforge --help` with `aforge do --help` sees two conventions for one flag and has to guess whether both work (they do) — set a `flags.Usage` that prints the double-dash form — sev: low — evidence: docs/design/polish/frames/cli-help.txt vs cli-subcommand-help.txt

24. The `--json` object is described only in a repository design document — docs/HEADLESS.md, and cmd/aforge/do.go:110-176 in comments — nothing compiled into the binary describes the fields, and `internal/manual/chat/` has no page about `aforge do`, `--json`, or the exit codes, so the manual the binary carries cannot answer the most common headless question — add a headless page to the corpus, or name the field list in the `do` flag help — sev: low — evidence: `grep -rln 'spend_overhead\|blocked_on' internal/manual/` returns nothing

25. Two query commands report a miss as a success — cmd/aforge/why.go (`bogus-node-id has no transcript…`, exit 0) and logs.go (`no row in this log carries a run id yet`, exit 0) — a script asking whether a node or a run exists cannot tell "not found" from "found and empty" without parsing prose; `notebook retract 999` gets this right with a non-zero exit — sev: low — evidence: docs/design/polish/frames/cli-errors.txt

---
26. `exec --turns` takes a bad count without saying which flag, what was given, or what it takes — cmd/aforge/exec.go — THIS IS THE HALF OF ROW 20 THAT IS STILL OPEN, and it is its own row because the ledger has no half-row granularity: a row that reads CLOSED with a live defect inside it is the ledger lying, which is the one thing it may not do. `logs --tail` already took the fix (`newCountFlag` in `count.go`); this is the one-line adoption of it. — sev: med — frames: n/a
27. `aforge why <bad-id>` reports a miss as a success — cmd/aforge/why.go — THE HALF OF ROW 25 THAT IS STILL OPEN, split out for the same reason. It prints its sentence and returns nil, so a script reads exit 0 and concludes the id exists and has nothing in it. `logs` took the same one-line change (`return exitCannotRun`); this is `why` taking it. — sev: med — frames: n/a
28. `aforge rebuild` asks its question on stdout, and the structural test cannot see it — cmd/aforge/rebuild.go, cmd/aforge/main.go (`usageText`) — THE REST OF ROW 19. The prompt (`Rebuild every materialized view in <path> from the event journal?` / `[y/N] `) goes to the command's `output` PARAMETER, which is `os.Stdout`, so a question lands in the pipe — the exact defect row M28 was about — and `TestNoDoorPrintsItsCommentaryToStdout` slips it because it scans for named answer streams and this one is a parameter. Two halves: send the question to the aside, and teach the test to follow a writer that arrives as an argument. `materialized view` and `derived table` are machinery vocabulary in both the prompt and `usageText`. — sev: med — frames: n/a

## fixed

Landed on `ui/polish-v0`. Every row below was verified by re-running the command in
its evidence column against a rebuilt `bin/aforge` and saving the output beside the old
capture. `go build ./...` is clean; `go vet ./cmd/aforge/ ./internal/config/` is clean.

| row | files changed | test | before → after |
| --- | --- | --- | --- |
| 1 | new `cmd/aforge/usage.go` (the one seam); `cache.go`, `chatv3.go`, `chatv3_at.go`, `competence.go`, `do.go`, `doctor.go`, `engine.go`, `exec.go`, `logs.go`, `main.go`, `notebook.go`, `rebuild.go`, `run.go`, `services.go`, `subharness_run.go`, `wake.go`, `why.go` — all eighteen flag sets now go through `commandFlags` + `parseCommandFlags` | `TestAskingForHelpIsNotAFailure` | `cli-help-exitcodes.txt` → `cli-help-exitcodes-after.txt` |
| 2 | same seam — `flag.ErrHelp` is intercepted before the `error:` line, usage goes to stdout, exit 0 | `TestAskingForHelpIsNotAFailure` | `cli-subcommand-help.txt` → `cli-subcommand-help-after.txt` |
| 3 | new `cmd/aforge/plainwords.go`; `do.go` (`refusalWords`, `failedErrand`), `exec.go` (`execFailureWords`), `main.go` (the default arm of `execute`) | `TestNoGoErrorChainReachesAPerson`, `TestAnUnrecognisedCauseIsSaidPlainlyAndNothingIsInvented`, `TestPlainWordsKeepsWhatAPersonCanActOn`, `TestAChainOfNothingButVerbsIsStillSaid` | `cli-badmodel.txt` → `cli-badmodel-after.txt` |
| 4 | `exec.go` — `execEnvelope.Error`, `buildExecEnvelope(outcome, runErr)` built once; `exec_test.go` call sites | `TestExecJSONSaysWhyTheRunFailed` | `cli-json-failure.txt` → `cli-json-failure-after.txt` |
| 5 | `do.go` (`errandFooter`, and the separator that no longer prints over nothing); `internal/config/settings.go` — `spentFigure` exported as `SpentFigure` with the emptiness law inside it, so there is one answer to "how is a spend written" | `TestTheHeadlessFooterLeavesOutWhatIsZero`, `TestTheFooterWritesASpendTheWayEverythingElseDoes` | `cli-badmodel.txt` → `cli-badmodel-after.txt` |
| 6 | `main.go` — the exec block of `usageText` carries the six-rung ladder, and every per-command help is a reading of that block | `TestExecsExitLadderIsWrittenWhereACallerLooks` | `cli-subcommand-help-after.txt` |
| 8 | `usage.go` (`unknownCommand`, `nearestCommand`, `editDistance`), `main.go` dispatch | `TestAMisspelledCommandNamesTheNearestOne` | `cli-unknown-and-version.txt` → `cli-unknown-and-version-after.txt` |
| 9 | `main.go` (`readText`/`readPipedText` carry the command name, `noGoalGiven`), `do.go`, `exec.go`, `brief_test.go` | `TestAMissingGoalShowsTheCommandAndNotTheWholeTable` | `cli-errors.txt` → `cli-errors-after.txt` |
| 10 | `usage.go` (`askedForHelp`, `commandHelp`), `main.go` (`runShow`), `models.go`, `cache.go` | `TestProbingAFlaglessCommandWithHelpIsNotAnError` | `cli-subcommand-help-2.txt` → `cli-errors-after.txt` |
| 11 | the seam — the flag package's own output is discarded in one place and the refusal is printed once, with the command's usage under it | `TestABadFlagIsRefusedOnceAndOnStderr` | `cli-errors.txt` → `cli-errors-after.txt` |
| 13 | `main.go` (`execute` answers `config.ErrNoAPIKey` at the one exit), `chat.go` (the duplicate pair removed), `plainwords.go` (the same remedy on the `--json` path) | `TestAMissingKeyIsAnsweredOnceWithTheRemedy` | `cli-nokey.txt` → `cli-nokey-after.txt` |
| 15 | `doctor.go` (spend and standing rows), `notebook.go` (the rail line), both through `config.SpentFigure`; `doctor_test.go` and `notebook_test.go` updated where they pinned the zeros | `TestDoctorDoesNotPrintAZeroItNeverMeasured`, `TestTheNotebookDoesNotPrintAZeroSpend` | `cli-readonly-commands.txt` → `cli-readonly-commands-after.txt` |
| 16 | `do.go` — the record is announced only for a run that was admitted (the brain built), and the empty folder is removed | `TestARunThatNeverStartedKeepsNoRecord` | `cli-nokey.txt` → `cli-nokey-after.txt` |
| 18 | `main.go` — `--debug` and `--no-host` are in `usageText` on the commands that take them | `TestTheUsageNamesEveryFlagAPersonCanType` | `cli-subcommand-help-after.txt` |
| 23 | `usage.go` (`flagRows`) — two dashes for a word, one for a single letter, which is exactly what the table already spells | `TestEveryFlagIsSpelledTheWayTheUsageSpellsIt` | `cli-subcommand-help-after.txt` |
| 7 | `cmd/aforge/main.go` (`usageText`, `environmentText`, the `helpWidth`/`helpTextColumn` law), `cmd/aforge/envelope.go` (`exitLadderHelp`, `foldedExitLadder`), `cmd/aforge/usage.go` (the closing line under every per-command page) | `TestEveryHelpPageFitsAnEightyColumnTerminal`, `TestTheHelpPageCostsFewerRowsThanTheOneItReplaced` | `frames/help-before.80x24.txt` (108 lines, **167 rows**, folded mid-word) → `frames/help-after.80x24.txt` (110 lines, **110 rows**); `frames/help-env-before.80x24.txt` (83 rows) → `frames/help-env-after.80x24.txt` (73) |
| 24 | closed earlier in the wave and **verified here, not assumed**: `internal/manual/chat/running-from-the-terminal.md` documents every key of the one envelope and the exit ladder, and `docs/HEADLESS.md` was rewritten onto both | `TestTheTerminalPageDoesNotSayASavedProgramReportsZeroSteps` (ten keys), `TestHeadlessDocumentsTheLadderAndTheEnvelopeItActuallyHas` | the row's own evidence line now answers: `grep -rln 'spend_overhead\|blocked_on' internal/manual/` names `running-from-the-terminal.md` |

The manual was updated in the same change, as the manual law requires:
`internal/manual/chat/commands.md` gains two sections — `--help` on any command and what a
mistyped command answers — and `internal/manual/chat/adaptive-runs.md` now says that a zero
part of the headless footer is left out, that a run which fell over at the door keeps no
record, and that `exec --json` carries `error` alongside its six-rung exit ladder.

### skipped, and why

The numbers below are SPELLED AS WORDS on purpose: `scripts/ledger.py`
reads `Row <digit>` or a bold bare digit anywhere under `## fixed` as a closure,
and every row in this list is one that is NOT closed.

- **Row twelve** (filesystem failures reach the person as wrapped syscall text) — `plainWords` now
  keeps the actionable half of those chains, but the row asks for the message to name *which
  flag* was wrong (`-w names /nope/dir, which does not exist`). That needs a per-site
  decision at six doors about which flag owns which path, which is a design call this lane
  cannot make from the audit.
- **Row fourteen** (doctor says nothing about a missing key) — needs a new reading in `internal/config`
  that reports whether a key is configured *and where it came from* (environment, profile
  file). Designing that seam is not something the audit settles.
- **Row seventeen** (a bad subharness name reported as an empty input) — the comment at
  `subharness_run.go:70` says the ordering is deliberate; changing it is a call for whoever
  owns that ordering.
- **Row nineteen** (`rebuild`'s storage vocabulary and its prompt) — needs somebody to decide what
  `aforge rebuild` discards *in plain words*, which is a product sentence, not a mechanical
  fix.
- **Row twenty** (`--tail notanumber` says `parse error`) — wants a `flag.Value` with a sentence on
  each numeric flag across `logs` and `exec`; worth doing, but it is its own small pass.
- **Rows twenty-one, twenty-two and twenty-five** — all `sev: low`, and outside the brief for this lane.
  (Rows twenty-one and twenty-two were later closed by the streams lane; see `audit-commands.md`'s
  row 13.)

---

## fixed — the failures a person meets

A third lane, on the rows the first two left: the ones where somebody has already
hit a wall and the wall answers in a library's words. Every fix below carries a
named test that was **watched to fail with the fix reverted** — the check is
recorded per test because five tests in this wave shipped green against the very
defect they named. Captures are `frames/err-<command>-{before,after}.txt`, taken
by running `bin/aforge` against a throwaway home.

**Row 12 — filesystem failures reach a person as Go wrapped chains over raw
syscall text.**
File: `cmd/aforge/plainwords.go` (`filesystemFault`, `filesystemSentences`,
`syscallVerbs`, `creatingVerbs`) — **and no door at all**, which is the point:
every command in this binary reports its failure through the one line in
`main.go`, so the five doors the row names are answered without one of them being
touched.
The rule is STRUCTURAL rather than a list of sites: a chain that ENDS in a
syscall fault (`<verb> <path>: <reason>`, which is exactly how `os.PathError`
formats itself) is a chain whose whole front is the binary narrating what it was
doing when the disk said no. The tail names the path, so the front carries
nothing anybody can act on and is dropped whole; the tail is respelled with a
cause and a remedy.

| before | after |
| --- | --- |
| `open notebook: stat /nope/graph.db: no such file or directory` | `there is nothing at /nope/graph.db.` / `check the path, and make the folder above it first if it is meant to be new.` |
| `open receipts: stat /nope/graph.db: no such file or directory` | the same two lines |
| `open /nope/graph.json: no such file or directory` | the same two lines |
| `create chat workspace: mkdir /nope: permission denied` | `/nope could not be created — this account is not allowed to write there.` / `choose a path you own, such as one under your home directory.` |
| `mkdir /nope: permission denied` (`exec`, which did not even name the flag) | the same two lines |

A LOOKING VERB AND A MAKING VERB ARE DIFFERENT ANSWERS TO ONE REASON: `stat
/x: no such file or directory` means the thing is not there, and `mkdir /x: no
such file or directory` means the folder ABOVE it is not — telling somebody to
check the path they typed would be telling them the wrong thing.

**What is deliberately NOT claimed is which flag named the path.** Six doors take
a path under four flags, and a sentence that guessed `--dir` at a door whose flag
is `--db` would send somebody to the wrong knob with confidence. That half of the
row stays open and is the reason it was skipped once already; what is closed is
the syscall text, the wrapped chain, and the missing remedy.
Test: `TestADiskFaultIsSaidInAforgesWordsAndSaysWhatToDo` (five chains,
sub-tested), `TestAPathThatCouldNotBeMadeIsNotToldItIsSimplyMissing`. **Both
watched to fail against the defect** — the first names each machinery word still
reaching the reader, the second names the two sentences that were identical.
`TestASentenceThatIsNotADiskFaultIsLeftAlone` is a GUARD and passes either way,
which is said here because a test that cannot fail is not evidence.
A sibling assertion pinned the old wording and was rewritten:
`TestJSONPrintsAnObjectWhenTheErrandCannotEvenStart` looked for the words `store
directory` — `do.go`'s wrapping verb. It now asserts the sentence names the
blocked PATH and carries the remedy, which is what the row is actually about and
is a stronger claim than the one it replaced.
Capture: `frames/err-notebook-{before,after}.txt`, `err-show-{before,after}.txt`,
`err-do-dir-{before,after}.txt`, `err-exec-dir-{before,after}.txt`.

**Row 14 — `aforge doctor` says nothing about a missing key.**
Files: `cmd/aforge/doctor.go` (`keyReport`, `readKeyReport`, `formatKey`,
`fallbackKeyEnv`).
Doctor is what somebody runs when nothing works, and on a machine with no
provider key it reported six healthy-looking rows and left with 0. There is now
a `key` row, and it is **first**, because it is the answer to the question the
command was opened with:

```
key              none · export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.
key              set · OPENROUTER_API_KEY
key              set · OPENAI_API_KEY
key              set · /home/x/.aforge/config.json
```

It names WHERE the key came from and never what it is — a key is a secret, and a
page nobody can paste into a defect report is a page that does not get pasted.
The remedy is `remedyFor(config.ErrNoAPIKey.Error())`, the same sentence every
door answers a keyless run with, so the page a person opens when nothing works
and the refusal they just read cannot say two different things.
**The row is printed when it is empty**, which is the one deliberate exception
here: the emptiness law is about a measurement nobody made, and this is a
measurement that came back "none". **The exit code stays 0** — doctor is a
report, and the row is the answer.
Test: `TestDoctorSaysThereIsNoKeyAndWhatToTypeAboutIt`,
`TestDoctorNamesWhereTheKeyCameFrom` (three rungs, and that the key itself never
appears), `TestDoctorAgreesWithTheDoorAboutWhetherThereIsAKey` (the seam:
`readKeyReport` climbs the rungs one at a time so it can say which answered,
while `config.APIKeyAt` folds them — this pins the two at every rung so a ladder
that grows a step is red here rather than a doctor quietly telling a working
machine it has none), `TestDoctorNamesTheSameKeyVariablesTheDoorDoes`.
**The first two were watched to fail with the row taken back out** — "doctor has
no `key` row at all, so a machine that cannot call a model reads as healthy",
printing the six rows it drew instead. The last two are guards and say so.
Capture: `frames/err-doctor-{before,after}.txt`.

**Row 17 — a bad subharness name is reported as an empty input.**
Files: `cmd/aforge/subharness_run.go` (`checkSubharnessName`,
`noSuchSubharnessNamed`).
`aforge run nosuchharness --input -` answered `the input is empty — there is
nothing here for the run to do` and never mentioned the name, so somebody who
had misspelled a program went away and fixed their input. Two things were wrong
and the message named the one they had got right.
The name is checked at the door now, and it costs nothing: the programs a person
can name are the bundles in two stores, read off a directory listing with no
provider connection, no toolbox and no workspace. The project store is found
where the run will WORK — through `errandWorkspace`, the same reading the run
itself uses — so `--dir` cannot make the door and the run disagree. A store that
will not list leaves the whole question to the full lookup inside the run, which
is still the authority; a pair that reads cleanly and holds nothing is an ANSWER
and refuses. Both readings say it through one function so they cannot say it two
ways.

| before | after |
| --- | --- |
| `error: the input is empty — there is nothing here for the run to do` | `error: there is no subharness called "nosuchharness", and this build has none to offer` |
| | (with programs saved: `there is no subharness called "nosuchharness" — this build has: formatter, tidy-up`) |

Test: `TestABadProgramNameIsSaidBeforeTheInputIsBlamed` — it makes BOTH things
wrong at once, so the two refusals are genuinely in a race and the test is about
which one answers. **Watched to fail**: `the refusal never names what the person
typed. said: "the input is empty — …"`. Also
`TestAProgramThatIsReallyThereGetsPastTheNameCheck` (the half that makes the
check safe to have — a real program, and the generalist, are admitted),
`TestBothReadingsRefuseAMissingProgramInTheSameWords`, and
`TestTheEarlyNameCheckReadsEveryBundleStoreTheRunRegisters` — structural, reading
`subharness_run.go` with `go/ast` and counting `UseBundles` calls, so the day the
packed trailer the seam comment anticipates is registered, an early reading that
had not heard of it fails here instead of refusing a program that exists.
A sibling law had to learn the new wall: `run_road_test.go`'s `roadTaken` told
the program road from the plan road by the program runner's first wall, `this run
needs its input`. The program runner has two first walls now and either one
identifies it — neither can be forged by the plan road, which never resolves a
program name at all. The law it pins (a bare word is a saved program's name in
every directory) is unchanged and still green from both directories.
Capture: `frames/err-run-subharness-{before,after}.txt`.

**Row 20 — `logs --tail notanumber` answers `parse error`.**
Files: `cmd/aforge/count.go` (new — `countFlag`, `newCountFlag`),
`cmd/aforge/logs.go`.
`parse error` is `strconv`'s message reaching a person through two layers
neither of which wrote it for anybody to read. The flag now says WHICH FLAG,
WHAT WAS GIVEN and WHAT IT TAKES, which is what `wallFlag` has always done for
`--timeout` in the same binary.

| before | after |
| --- | --- |
| `error: invalid value "notanumber" for flag -tail: parse error` | `error: invalid value "notanumber" for flag -tail: a whole number of calls to show, such as 40` |
| `--tail -5` was accepted and became a slice-arithmetic surprise | `invalid value "-5" for flag -tail: a whole number of calls to show, and not a negative one` |

The flag carries its own NOUN and not a shared one — what a number means differs
per flag — and its `example` is the door's own default, so the figure offered is
one the flag really accepts. **Zero stays legal**: asking for none of something
is a question, not a typo.
Test: `TestABadCountFlagSaysWhichFlagWhatWasGivenAndWhatItTakes` (all three
things by name), `TestANegativeCountIsRefusedWhereTheFlagCanStillBeNamed`.
**Both watched to fail** with `flags.Int` put back — the first printed `parse
error` under the whole flag list, the second accepted `-5`.
**NOT CLOSED HERE: `exec --turns`,** which the row names in the same breath.
`cmd/aforge/exec.go` is another lane's file this wave. `newCountFlag` is a
one-line adoption (`flags.Int` → `newCountFlag(flags, "turns", …, "turns", …)`)
and is waiting for whoever holds it.
Capture: `frames/err-logs-tail-{before,after}.txt`.

**Row 25 — a query command reports a miss as a success (the `logs` half).**
File: `cmd/aforge/logs.go`.
`aforge logs --run <id>` for a run that is not in the log printed a sentence and
left with 0, so a script asking whether a run exists could not tell "not found"
from "found, and it made no calls" — and the two mean opposite things.
The rung is `exitCannotRun` off the one ladder in `envelope.go`, which is where
the ladder moved to an hour before this lane started: the question was asked and
could not be answered, nothing ran and nothing was spent. The sentence stays on
stdout and the code is returned bare, because the reason is already written for a
person on the line above and `error:` in front of it would be the same fact
twice. `aforge notebook retract 999` has always had this right.

| before | after |
| --- | --- |
| `no row in this log carries a run id yet` · **exit 0** | the same sentence · **exit 1** |
| `no calls for run r-none` · **exit 0** | the same sentence · **exit 1** |
| `no call deadbeef in this log` · **exit 0** | the same sentence · **exit 1** |

A FILTER THAT MATCHED NOTHING IS NOT A MISS, and that half is pinned too: only
`--run` and `--call` are ids somebody pasted believing they exist. A `--tag`, a
`--model` or a `--node` that matched nothing is a search that came back empty,
which is an answer; a bare listing on a quiet machine is a quiet day; and
`--json` never earns a sentence or a rung, because an empty stream is exactly
what "no matching rows" looks like to a parser.
Test: `TestAnIdThatIsNotInTheLogIsNotReportedAsSuccess` (both ids, asserting the
RUNG by name and that the sentence survives), `TestASearchThatMatchedNothingIsNotAMiss`.
**The first was watched to fail** — `a lookup that found nothing left with
success. err: <nil>`. The second is the guard.
Two sibling assertions treated any non-nil return as a fault and were rewritten
to read past the code, through one `isMissRung` helper that says what it is
looking for: `TestLogsFiltersByRunAndSaysNothingForARowThatHasNoRun` and
`TestLogsTellsAnEmptySearchApartFromAQuestionItCannotAnswer`.
Capture: `frames/err-logs-run-{before,after}.txt`.

**Row 21 — a column header over no rows (the `notebook` half).**
File: `cmd/aforge/notebook.go` (`writeNotebookRows` lifted out).
`SEQ SCOPE KIND AGE USES RIDES BAD STATUS BELIEF` over nothing was the entire
output of this command on a fresh machine. Nine column names with no rows read
as a table that failed to load. `aforge notebook` now answers

```
the notebook is empty — hand aforge some work, and what it learns lands here.
```

— and the empty state says WHAT TO DO NEXT, which is the half a flat "nothing
here" leaves out. The header returns the moment there is a row: the rule is that
a header is never drawn WITHOUT one, not that the table went away.
The `why self` half of this row was closed earlier by the streams lane.
Test: `TestAnEmptyNotebookSaysSoInsteadOfPrintingAHeader` (**watched to fail** —
`the column header "SEQ" is drawn over no rows at all`, printing the header and
the rail line beneath it), `TestTheNotebookHeaderReturnsAsSoonAsThereIsARow`.
Capture: `frames/err-notebook-empty-{before,after}.txt`.

**Row 22 — `aforge services` prints nothing, and its rows are raw tab-separated
fields.**
File: `cmd/aforge/services.go`.
The first half — silence on a healthy machine — was closed by the streams lane
and is verified here against a rebuilt binary: `nothing is being kept running.`
The second half was still open. The rows were five raw tab-separated fields with
no header and no alignment, which is a machine's shape printed at a person and
was the only listing in the binary that had it.

```
dev-server\trunning\t2h\tport:5173\t/tmp/dev.log
docs-preview-server\trunning\t2h\tport:8080\t/tmp/docs.log
```

```
NAME                 STATUS   AGE  HEALTH     LOG
dev-server           running  2h   port:5173  /tmp/dev.log
docs-preview-server  running  2h   port:8080  /tmp/docs.log
```

Same `tabwriter` `notebook` and `why` draw with, so a person reading two
listings reads one shape. `aforge services stop <name>` gave up its own tab pair
for the register every other one-line answer speaks in: `dev-server · stopped`.
Test: `TestServicesRowsAreAlignedUnderAHeader` — it uses two names of very
different lengths and asserts the STATUS and LOG columns start at the same index
in both rows, so an alignment that only worked on equal-width names fails.
**Watched to fail** with the `Printf` put back: `the rows are still raw
tab-separated fields: "dev-server\trunning\t…"`.
`TestServicesDrawsNoHeaderWhenNothingIsRunning` is the guard on the streams
lane's half. `TestServicesCommandListsAndStops` pinned the old stop receipt by
equality and was updated.
Capture: `frames/err-services-rows-{before,after}.txt` (the before is
reconstructed from the pre-change source and the exact bytes the reverted test
prints — the binary that emitted them is gone), `err-services-{before,after}.txt`.

### Not fixed here, and why

- **Row nineteen — `aforge rebuild`.** Half of it was already closed by another
  lane: the receipt counts `steps` rather than `nodes`, and the transcript note is
  signed `aforge` rather than `the harness`. **What remains is entirely inside
  `cmd/aforge/rebuild.go` and `usageText` in `cmd/aforge/main.go`, both held by
  other lanes this wave**, so it is reported rather than done. What remains, exactly:
  the prompt still reads `Rebuild every materialized view in <path> from the event
  journal?` and `The journal itself is untouched; everything derived from it is
  discarded and replayed. [y/N] `; `usageText` still says `discard every derived
  table and replay the journal`; the prompt is written to the command's `output`
  writer, which is `os.Stdout` — a question in the pipe, exactly the defect M28
  fixed at `cache clean`, and it slips past
  `TestNoDoorPrintsItsCommentaryToStdout` because the writer is a parameter rather
  than a named answer stream; and with stdin closed, declining still returns
  `fmt.Errorf("rebuild cancelled")`, so saying no is reported as an error with a
  non-zero exit.
- **Row twenty-five, the `why` half.** `cmd/aforge/why.go` is another lane's file.
  `aforge why <node-id>` for an id that has no transcript still prints its
  sentence and returns nil. The change is the same one `logs` took: `return
  exitCannotRun` after the sentence.
- **Row twenty's `exec --turns` half** — `cmd/aforge/exec.go` is another lane's
  file. `newCountFlag` is there for it.
- **Row twelve's "which flag was wrong" half** — a per-site decision at six doors
  about which flag owns which path, which is a design call the audit does not
  settle. The syscall text, the wrapped chain and the missing remedy are closed.
- **Rows seven, twenty-three and twenty-four** are other lanes' or other passes'.


---

## fixed — the developer lane's three (rows 26, 27, 28)

Landed on `ui/polish-v0` alongside `audit-dev.md`'s seven. Every test below was checked by
reverting its fix and watching it fail.

**Row 26 — `exec --turns` takes a bad count silently.** `cmd/aforge/exec.go` — both numeric
walls adopt `newCountFlag` (`count.go`), which is what `logs --tail` already uses. Test:
`TestABadCountNamesTheFlagAndWhatItTakes` in `cmd/aforge/usage_test.go`.

`--token-budget` went with `--turns` rather than being left as an identical defect one line
below it; the hidden old spellings `--turns` and `--budget` write through to the same values
and are refused in the same words.

| | |
| --- | --- |
| before | `error: invalid value "notanumber" for flag -turns: parse error` |
| after | `error: invalid value "notanumber" for flag --turns: a whole number of turns to allow, such as 200` |

`frames/cli-C26-before.txt` → `frames/cli-C26-after.txt`.

**This row had a second half nobody had looked for.** `internal/manual/truth_test.go`'s
`flagNumber` reads a flag's default out of the source with a regex matching `flags.Int(…)`
only, so `--max-turns`'s figure — which `models-and-cost.md` quotes — went unreadable the
moment the flag changed shape, and `logs --tail` would have done the same. The reader was
taught the new shape rather than the flag being exempted: exempting it is how a whole class
of quoted figures goes silently unchecked. The default did not move, so no page did.

**Row 27 — `aforge why <bad-id>` reported a miss as a success.** `cmd/aforge/why.go`
(`writeNodeTranscript` returns `exitCannotRun`), `internal/manual/chat/running-from-the-terminal.md`.
Tests: `TestAskingWhyAboutAnIdThatIsNotThereIsNotASuccess` in `cmd/aforge/why_test.go`, and
`TestWhySaysSoWhenThereIsNoTranscript` in `transcript_test.go` now asserts the rung beside
the sentence. The sentence is unchanged — it is for the person; the exit is for the script.
`frames/cli-C27-before.txt` (exit 0) → `frames/cli-C27-after.txt` (exit 1).

**Row 28 — `aforge rebuild` asked its question in the pipe, and the structural test could
not see it.** `cmd/aforge/rebuild.go` (the question and `cancelled` go to `aside`; the
result line stays on stdout), `cmd/aforge/main.go` (`usageText`), `cmd/aforge/run.go`,
`cmd/aforge/streams_test.go`, `internal/manual/chat/running-from-the-terminal.md`,
`docs/HEADLESS.md`. Tests: `TestTheRebuildQuestionIsAnAsideAndNotInThePipe`
(`cmd/aforge/rebuild_test.go`) and `TestNoDoorPrintsItsCommentaryToStdout`.

**The mechanism in the row is not the one that was there, and it matters.** The prompt did
arrive through a writer passed as a PARAMETER — and that was never the problem: the
parameter is named `output`, which is one of `answerWriters`, so the call WAS scanned. What
the scan could not see was the SHAPE. The question mark ended its own line, and the half
that actually waits for a keystroke ends `[y/N] ` — no colon, no question mark, nothing the
two existing rules recognise. A bracketed-choice rule is what closes it, and it was checked
both ways: with the rule and the defect restored the test FAILS; with the old rules and the
defect restored it goes GREEN, which is the gate slipping exactly as the row said.

The new rule immediately found a second instance nobody had filed: `run.go`'s
`authorizeHeadlessRail` writes `Continue? [y/N] ` to a parameter named `output` whose only
caller has always passed `os.Stderr`. A writer whose name says stdout and whose value is
stderr is how the next person threads the wrong one in, so the parameter is `commentary`
now — every line that function writes is an aside.

`materialized view` and `derived table` are gone from the prompt, from `usageText`, from
the manual page and from `docs/HEADLESS.md`. What `rebuild` throws away is **everything
aforge worked out from the journal**.

| | |
| --- | --- |
| before (stdout) | `Rebuild every materialized view in <path> from the event journal?` / `The journal itself is untouched; everything derived from it is discarded and replayed. [y/N] cancelled` |
| after (stdout) | *empty* |
| after (stderr) | `Rebuild everything aforge worked out from the journal in <path>?` / `The journal itself is untouched; everything worked out from it is discarded and replayed. [y/N] cancelled` |
| after (stdout, with `--yes`) | `rebuilt 1 steps from 2 journaled events` |

`frames/cli-C28-before.stdout.txt` → `frames/cli-C28-after.stdout.txt` and
`frames/cli-C28-after.stderr.txt`.

**Row nineteen is answered by the above and is left for whoever owns it to close.** Its
skip note said it needed somebody to decide what `aforge rebuild` discards in plain words;
that decision is made and shipped here — *everything aforge worked out from the journal* —
and the prompt no longer runs into the answer stream. The number is spelled as a word so
`scripts/ledger.py` does not close a row this lane was not given.
