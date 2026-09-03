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

The manual was updated in the same change, as the manual law requires:
`internal/manual/chat/commands.md` gains two sections — `--help` on any command and what a
mistyped command answers — and `internal/manual/chat/adaptive-runs.md` now says that a zero
part of the headless footer is left out, that a run which fell over at the door keeps no
record, and that `exec --json` carries `error` alongside its six-rung exit ladder.

### skipped, and why

The numbers below are SPELLED AS WORDS on purpose: `scripts/ledger.py`
reads `Row <digit>` or a bold bare digit anywhere under `## fixed` as a closure,
and every row in this list is one that is NOT closed.

- **Row seven** (`aforge --help` wraps mid-word at 80 columns) — re-laying the whole 127-line block
  is a typographic pass over text this wave is also editing; it wants doing on its own so
  the diff is readable.
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
- **Rows twenty-one, twenty-two, twenty-four and twenty-five** — all `sev: low`, and outside the brief for this lane.
