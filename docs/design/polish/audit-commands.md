# The command surface, audited against COMMANDS.md

Rows are `WHAT — file:line — what it costs a developer — fix shape — sev — evidence`.
The design they are measured against is `COMMANDS.md` beside this file.

`docs/design/polish/audit-cli.md` already carries 25 rows from an earlier pass of
this wave. Rows there are not repeated here: `charter` and `$0.00` in `doctor`
(cli-15), `services` printing raw tab-separated fields (cli-22), the bare
`TRIED COST LEARNED` header (cli-21), `rebuild`'s wording (cli-19), and the
`--json` shape being documented only in `docs/HEADLESS.md` (cli-24) are theirs.
Where a row below stands next to one of those, it says so and says what it adds.

Four further things are deliberately **not** here because other lanes own them in
this wave: `--help` exiting 1, Go error chains reaching people, `exec --json` carrying
no error field, and the chat palette / `/help` / search door. Rows below assume
those are fixed.

Captures were taken with a binary built from `HEAD` (`25e72ff41`) into the
scratchpad, because the shared tree is mid-edit by another lane and does not
compile. Where a capture shows a `--help` exiting 1, that is the other lane's
row and not the point of mine.

`cmd/aforge/` is being edited by another lane while this was written, so line
numbers move under it. Every citation below was checked against the working tree
at the time of writing; if one has drifted, the quoted string in the row is the
thing to grep for.

Counts: **10 high · 15 med · 4 low · 29 rows.**

---

1. `--help` opens by calling the product "build and revise task graphs" — cmd/aforge/main.go:233 — the first line a developer reads describes a static pipeline that is four of twenty-three verbs, and does it in machinery words; somebody evaluating aforge decides what it is from this sentence — replace with the manual's own sentence: `aforge — an agent you talk to, and hand work to when you walk away`; the same line is the package doc at main.go:1-6 and must change with it. Manual: `starting-aforge.md` already says this and needs no edit — sev: high — evidence: `docs/design/polish/frames/cmd-help.txt` line 1

2. `aforge run --help` says "leaf" or "leaves" six times — cmd/aforge/run.go:39,41,42,43,44,45 — `leaf` is machinery vocabulary and appears in the help of the one command whose flags a developer must get right; nobody outside this repository knows a leaf is a step, so `--turns` and `--budget` cannot be reasoned about at all — respell every one as *step*: "how many steps may run at once", "backstop on turns per step", "token budget per step", "nothing new starts and steps in flight land", "write a working method for each step before running". Manual: no page names these flags yet; they land in the new `running-from-the-terminal.md` (row 30) in the new words — sev: high — evidence: `docs/design/polish/frames/cmd-run-help.txt`

3. `aforge why <node-id>` help says "show what one leaf actually did" — cmd/aforge/main.go:299 (usageText) — same leak on the one command a person reaches for when a task went wrong, and `<node-id>` is not a noun this product otherwise has — "show what one piece of work actually did: its turns, the tools it called with what arguments, what came back, and how it ended", with the argument spelled `<task-id>`. Manual: covered by row 4 — sev: high — evidence: `docs/design/polish/frames/cmd-help.txt`

4. `why`, `notebook`, `competence`, `services`, `wake` and `rebuild` appear nowhere in `internal/manual/chat/` — cmd/aforge/usage.go:285 (`knownCommands`) — the manual is the only authoritative source about aforge for the model, so the chat cannot tell a person how to see what a task did, or how to free the store; it will improvise or deny — write `internal/manual/chat/running-from-the-terminal.md` with a `## ` heading per verb family from COMMANDS.md §2, and extend the manual gate (`internal/tui3/manual_test.go`'s shape) to iterate `knownCommands` so a verb added without a page fails the build. Manual: this row *is* a manual change — sev: high — evidence: `grep -rl "aforge why" internal/manual/chat/` returns nothing; same for notebook, competence, services, wake, rebuild

5. Two `--json` result envelopes with no shared vocabulary (cli-24 covers only that neither is documented) — cmd/aforge/do.go:107-138 and cmd/aforge/exec.go:23-28 — `do` calls the answer `deliverable`, `exec` calls it `text`; `do` reports `seconds`, `exec` reports `elapsed_ms`; `do` has `settled`, `exec` has `stop`; a harness wrapping both writes two readers and the second one is written wrong — one envelope for `do`, `exec` and `run`: `ok`, `stop`, `answer`, `files`, `error`, `spend_usd`, `tokens`, `seconds`, `model`, `steps`, built in one place the way `buildExecEnvelope` already is (exec.go:259). Keep the old field names as duplicates for one release. Manual: the shape is documented in the new terminal page — sev: high — evidence: the two struct blocks; `aforge do --help` and `aforge exec --help` describe different objects

6. Three exit-code tables, two of which contradict each other (cli-6 covers only exec's being undocumented) — cmd/aforge/do.go:84-93, cmd/aforge/exec.go:236-252, cmd/aforge/subharness_run.go:335-342 — `do` exit 1 means "nothing usable" and `run subharness` exit 1 means "could not be run at all"; `exec` returns 2,3,4,5,6 and never 1; a script that branches on the code branches wrong on at least one of the three — one table: 0 done · 1 could not run at all · 2 ran and did not finish · 3 a limit you set stopped it · 4 needs an answer and nobody was there. Which limit stopped it is already in `stop`. `AFORGE_EXIT_CODES=legacy` keeps exec's 2/3/4/5/6 for one release. Manual: the table goes in the new terminal page and in the change entry — sev: high — evidence: `docs/design/polish/frames/cmd-help.txt` (three different exit lines in one table)

7. `--budget` means tokens while "budget" means dollars everywhere else in the product — cmd/aforge/exec.go:46, cmd/aforge/run.go:43 — `AFORGE_DAILY_BUDGET`, `/budget`, `--max-cost` and the settings sheet all use *budget* for money; `--budget 150000` is a number a person will read as dollars exactly once, and that once is expensive — rename to `--token-budget`, keep `--budget` hidden for one release, and rename `--run-budget` (run.go:44) to `--total-token-budget` for the same reason. Manual: `models-and-cost.md` mentions `aforge exec`; the flag names go in the new terminal page — sev: high — evidence: `docs/design/polish/frames/cmd-exec-help.txt` beside the `AFORGE_DAILY_BUDGET  500  daily dollar rail` row of `cmd-help.txt`

8. `--timeout` is a duration on `do` and an integer of seconds on `exec` — cmd/aforge/do.go:186 vs cmd/aforge/exec.go:47 — `aforge do --timeout 15m` works and `aforge exec --timeout 15m` is a parse error; the flag with the same name and the same job takes two types, and the failure is at the door of a long unattended run — give `exec` the same duration `flag.Var` `do` already has (do.go:186), and give it to `graph run` and `wake` too. `--max-seconds` on wake (wake.go:88) becomes `--timeout`. Manual: the new terminal page states one duration form — sev: high — evidence: `aforge exec --help` shows `-timeout int`, `aforge do --help` shows `-timeout value … such as 15m or 2h`

9. `run` names two unrelated commands and help needs a special case to tell them apart — cmd/aforge/main.go:163 and cmd/aforge/usage.go:166 — `aforge run graph.json` and `aforge run subharness <name>` share a verb and share nothing else; `longerCommands` exists solely so `aforge run --help` does not print the wrong synopsis, which is the code admitting the overload — `aforge run <program>` becomes the one meaning (matching `/subharness <name>` in the chat) and the pipeline moves under `aforge graph plan|show|revise|run`; old spellings hidden one release, `longerCommands` shrinks to `cache clean`. This is the large rename and it is the right one. Manual: `saved-programs.md:187` ("Running one without the chat — aforge run subharness") and `subharnesses.md` both name the old form and must be rewritten in the same change — sev: high — evidence: cmd/aforge/usage.go:160-166 comment

10. The word `seat` reaches a person on stderr on four headless doors — internal/config/seats.go:331 — the inherited-crew notice reads "your crew was set before the worker seat existed"; `seat` is machinery vocabulary and this is the one line whose whole job is to explain a surprising model choice to somebody who did not expect it — respell as "your crew was set before the worker role existed · it is running on your <tier> model until you pick a crew again"; `Seat`/`Seats` stay as Go type names, which nobody reads. Manual: `commands.md:1291` calls `/crew` "the six-seat reading" and `commands.md:1606` "the roles rows in settings" — pick *role* in both — sev: high — evidence: internal/config/seats.go:327-333, printed by cmd/aforge/do.go:313, main.go:425, main.go:543, exec.go:96, subharness_run.go:86

11. `-w`, `-o` and `-j` have no long spelling at all (extends cli-23: the fix is new names, not a print change) — cmd/aforge/usage.go:110 vs cmd/aforge/main.go:253 — `flagRows` writes every flag with two dashes, so per-command help prints `--w` while the top-level table prints `-w`; a reader cannot tell which is real, and `--w` is a spelling no tool uses — add `--dir`, `--out`, `--parallel` as the printed names and keep `-w`, `-o`, `-j` as hidden aliases that never expire. Manual: the new terminal page uses only the long forms — sev: med — evidence: cmd/aforge/usage.go:110 `head := "  --" + f.Name`; `docs/design/polish/frames/cmd-help.txt` writes `[-w dir]`

12. `exec --plan-model` is accepted and documented as doing nothing — cmd/aforge/exec.go:49 — "accepted for headless model-pin parity; exec performs no planning"; a harness author reading the flag list reasonably concludes the flag has an effect, pins a planning model on a thousand calls and measures the wrong thing — remove it; if a caller passes it, say `exec does not plan — --plan-model has no effect here` on stderr and carry on. A flag that exists only because the code had a knob is the one thing this pass is for. Manual: no page names it — sev: med — evidence: `docs/design/polish/frames/cmd-exec-help.txt`

13. Nothing-to-show is answered three different ways, and there is no rule (extends cli-21 and cli-22) — cmd/aforge/cache.go:80 (a sentence), cmd/aforge/services.go:31-38 (silence), cmd/aforge/why.go:59-60 (a bare column header) — `aforge services` on a healthy machine prints absolutely nothing and exits 0, which is indistinguishable from a broken command; `aforge why self` prints `TRIED COST LEARNED` with no rows, which is a header claiming a table that is not there — one rule: a listing with nothing in it prints one short sentence, and a header is never printed without a row under it. `cache` has it right and is the model. Manual: the new terminal page states the rule once — sev: med — evidence: `docs/design/polish/frames/cmd-misc.txt` — `aforge services` → empty, exit 0; `aforge why self` → header only, exit 0; `aforge cache` → "the cache is empty · <path>"

14. `graph plan` and `graph run` print their preamble to stdout; `do` prints the same thing to stderr — cmd/aforge/main.go:424-425, cmd/aforge/run.go:143-152 vs cmd/aforge/do.go:313 — `aforge plan "x" -o p.json > /dev/null` is a reasonable thing to type and silently discards nothing useful, but `aforge plan "x" --json | jq` breaks the moment the preamble is not suppressed; the same fact is on two streams depending on the verb — move `goal:`, `workspace:`, `models:` and the panel line to stderr on `plan`, `revise` and `run`, as `do` already does. Manual: no page names them — sev: med — evidence: cmd/aforge/main.go:424 `fmt.Printf("goal:   …")`, cmd/aforge/do.go:313 `fmt.Fprintln(request.stderr, seats.Report())`

15. `run` reimplements the models line instead of calling the one helper — cmd/aforge/run.go:147-153 — `Seats.Report()` exists so "the four doors cannot label the same fact differently" (internal/config/seats.go:412) and `run` labels it `models:    ` with its own padding and its own notice placement; the next field added to `Report` will be missing here — call `seats.Report()` and let the door's own column width be a formatting concern, not a second copy of the sentence — sev: med — evidence: internal/config/seats.go:412-415 comment vs cmd/aforge/run.go:147

16. `--yes-spend` is documented as two different things on two commands — cmd/aforge/do.go:192 vs cmd/aforge/run.go:46 — `do` says "approve a plan whose price crosses the consent threshold", `run` says "preauthorize raising today's dollar rail when reached"; those are two different decisions and a developer setting the flag on both cannot tell which they authorized — one sentence: "approve spending past today's limit and past the plan-price question, without stopping to ask". `rail` is machinery vocabulary and goes with it. Manual: `models-and-cost.md` covers spending limits and should name the flag — sev: med — evidence: the two help strings, `cmd-do-help.txt` and `cmd-run-help.txt`

17. `--ensemble` is a tri-state integer with two magic values — cmd/aforge/main.go:385 — "0 decide from the goal, -1 never, N>=2 force N independent passes"; `-1` for off and `0` for auto is a code-shaped API, and `--ensemble 1` is undefined — `--passes auto|off|<n>`, defaulting `auto`. Manual: no page names it — sev: med — evidence: `docs/design/polish/frames/cmd-plan-help.txt`

18. `--contracts` and `--brief` are one concept under two machinery names — cmd/aforge/run.go:45 and cmd/aforge/main.go:384 — "write a per-leaf working method before executing" and "write a self-contained instruction for every leaf" describe the same thing at two stages, under two words neither of which means anything outside this repository; `--contracts` also defaults true and has no negative spelling, so turning it off requires `--contracts=false`, which nothing else in the binary needs — `graph run --no-method` and `graph plan --instructions`, both described as "a self-contained working method for each step". Manual: no page names them — sev: med — evidence: `cmd-run-help.txt`, `cmd-plan-help.txt`

19. `--context-fill` and `--completion-reserve` document themselves by the environment variable they set — cmd/aforge/do.go:195-200, cmd/aforge/exec.go:50-51 — "…; sets AFORGE_CONTEXT_FILL_PCT for this run" tells a reader about the implementation and nothing about the decision; and the sentence hard-codes "(default 60)" and "(default 65536)" while the flag's `DefValue` is 0, so the printed default and the real one are two facts that will drift — drop the env-var clause, and give the flags their real defaults so `shownDefault` (usage.go:134) prints them from the constants that own them, the way the environment table's dollar figures already are (main.go:228-232). Manual: no page names them — sev: med — evidence: `docs/design/polish/frames/cmd-do-help.txt`

20. `devices revoke --all` is hand-parsed, position-sensitive, and missing from the usage sentence — cmd/aforge/chatv3_at.go:312,328-331 — `--all` is only read when it is the *first* word after `revoke`, so `aforge devices revoke laptop --all` fails with a usage line that does not mention `--all` at all; the person is revoking access to their machine and is told the wrong grammar — give `devices` a real flag set through `commandFlags`/`parseCommandFlags` so `reorder` handles it like every other door, and put `--all` in both usage sentences. Manual: `reaching-this-machine-without-ssh.md` names `aforge devices` and must gain the `--all` form — sev: med — evidence: `docs/design/polish/frames/cmd-misc.txt` — `aforge devices --help` → `error: usage: aforge devices [revoke <name>]`, exit 1

21. The two surfaces use different words to confirm the same deletion — cmd/aforge/cache.go:88 vs internal/manual/chat/commands.md:724 — the terminal asks you to type `clean`, the chat wants `/cache clean now`; a person who learned one types the other at the one prompt in the product that deletes gigabytes — one word, `now`, in both, because the chat cannot pass a flag; `--yes` stays as the script's spelling. Manual: `commands.md:724` keeps `now` and gains the terminal form — sev: med — evidence: cmd/aforge/cache.go:88 `!= "clean"`; `docs/design/polish/frames/cmd-help.txt` "asks you to type \"clean\""

22. `brain` is the word `doctor` uses for the store, in output and in `--help` — cmd/aforge/doctor.go:226 and cmd/aforge/main.go:284 — `brain  ~/.aforge/graph.db · 496 KiB` and "show the brain, resident, watch, spend, and open counts"; a developer looking for where their data lives does not search for *brain*, and `--db`'s own help calls the same file "the durable graph database" — one noun, *store*: `store  <path> · 496 KiB`, and `--db` becomes "the store to work in". Manual: the new terminal page uses *store* — sev: med — evidence: `docs/design/polish/frames/cmd-doctor.txt`

23. `--help` has no examples — cmd/aforge/main.go:233-367 — a developer meeting a twenty-three verb CLI gets a synopsis grammar and no worked line; the two things they most want to copy — a one-shot piped into `jq`, and running against another machine — are nowhere on the page — five lines between the commands and the environment table, chosen so each teaches a different thing (COMMANDS.md §5). Manual: `starting-aforge.md:32` already has the launch table; examples are terminal-side — sev: med — evidence: `docs/design/polish/frames/cmd-help.txt` has no `Examples:` block

24. `--help` is 127 lines and more than half is the environment table (cli-7 covers its wrapping, not its size) — cmd/aforge/main.go:310-366 — the last thing on a person's screen after asking "what commands are there" is `AFORGE_CALL_LOG_BODIES`; the commands scroll off — move the table to `aforge help env`, leave a one-line pointer, and group the commands under the five headings of COMMANDS.md §2. Manual: `models-and-cost.md` documents the variables and gains a line naming `aforge help env` — sev: med — evidence: `wc -l docs/design/polish/frames/cmd-help.txt` → 127

25. The commands in `--help` are one flat list in no stated order, and `run`'s two forms are separated by `exec` — cmd/aforge/main.go:235-309 — `aforge run <graph.json>` is at line 24 of the table and `aforge run subharness` at line 34, with `exec` between them; the resident's verbs (`notebook`, `competence`, `services`, `wake`) sit in the same undifferentiated run as `chat` and `do` with nothing saying they belong to a different product — five headed groups, adjacent forms of one verb kept together. `knownCommands` (usage.go:285) already claims to be "in the order the table introduces them" and would follow. Manual: the new terminal page uses the same grouping — sev: med — evidence: `docs/design/polish/frames/cmd-help.txt` lines 17-27 and 33

26. `aforge manual --help` prints the page list instead of the command's usage — cmd/aforge/manual.go:45-52 — the argument for it is good (the list *is* what the command can be asked for) but it means one verb in the binary answers `--help` differently from the other twenty-two, and the flag no longer means "how do I call this" — print the one-line usage above the list. Nothing is lost and the gesture keeps one meaning. Manual: `commands.md:1702` documents `aforge manual` and gains the line — sev: low — evidence: `HOME=$DEMO_HOME aforge manual --help` prints the page list

27. `logs` prints its path header on stdout — cmd/aforge/logs.go:109 — the flag-suppression at :105 shows the author knew commentary should not be in the stream, but the fix was to special-case `--json` rather than to move the line; `aforge logs | grep -c .` is off by one, and `--path` exists precisely because that line is not the answer — write the header to stderr in both modes; the `--json` special case then disappears. Manual: the new terminal page states the stdout rule — sev: low — evidence: `docs/design/polish/frames/cmd-logs.txt` line 1 is the path

28. `cache clean` writes its confirmation prompt to stdout — cmd/aforge/cache.go:84-87 — a question is not an answer; `aforge cache clean | tee clean.log` shows the person a blank terminal waiting for a word they cannot see — prompt on stderr, the "cleaned · N freed" receipt on stdout. Manual: `commands.md:724` — sev: low — evidence: cmd/aforge/cache.go:84 `fmt.Fprintf(output, "This deletes the shared build cache…")`

29. `aforge version` prints only `aforge dev` — cmd/aforge/version.go:14 and internal/buildinfo — the comment says the command exists so an installer or a packaging script can tell *which* binary is on the PATH, and on a build cut from a checkout it cannot; a bug report carrying "aforge dev" names nothing — print the commit and the build date alongside the version when a tag is absent, which is what `debug.ReadBuildInfo` already carries. Manual: `running-on-another-machine.md` names `aforge version` — sev: low — evidence: `docs/design/polish/frames/cmd-misc.txt` — `aforge version` → `aforge dev`

---

## Already right — do not "fix" these

- **`--model` / `--plan-model`.** One shared help string naming the whole
  precedence ladder, on six doors (`cmd/aforge/models.go`, `modelFlagHelp`).
  This is the standard the rest of the flag table should be held to.
- **`parseCommandFlags` and `writeCommandUsage`** (cmd/aforge/usage.go:63,83).
  One seam, per-command usage lifted out of the one table so a synopsis cannot
  go stale, help on stdout, errors on stderr, the flag package's duplicate dump
  discarded once. The right architecture.
- **`shownDefault`** (usage.go:134) — the emptiness law applied to a usage page.
- **`reorder`** (main.go:679) — flags after positionals work on every door that
  takes a positional; `--` is honoured. The three doors that skip it take no
  positionals, so it would be a no-op there.
- **`logs`'s filter family** — `--run`, `--call`, `--tag`, `--model`, `--node`,
  `--body`. Exact, combining by AND, documented as such, with `--json` a
  byte-for-byte passthrough that suppresses every sentence. The best-designed
  command in the binary.
- **`engine` and `tick` hidden** from `--help` and from the typo suggester
  (main.go:135,179) — machinery a surface dials, argued in place.
- **`aforge manual` needing no key and spending nothing** (manual.go:1-20) — and
  refusing to truncate a page on the one surface where the whole of it is free.
- **`do`'s stream discipline** (do.go:2448) — the deliverable and a short footer
  on stdout, everything else on stderr, with the reason written down.
- **`doctor`'s emptiness handling**, as amended in the working tree
  (doctor.go:202-213) — `$0.00 today` gone, counts dropped when zero.
- **`--yolo` needing `--max-hours` or `--max-cost` before it will carry work on
  by itself** (chatv3.go:92-96) — a posture and a budget kept as two decisions.

---

## fixed

The rename lane. Every row below is closed in `cmd/aforge/`, with a named test
and a capture from a binary built on `ui/polish-v0` against a throwaway demo
home. `frames/cmd-help-before.txt` and `frames/cmd-renames-after.txt` are the
two to read first; the rename's own before/after table is written into
`envelope-and-exits.md` beside the sibling lane's, because a person with a
script needs both in one place.

**Row 9 — `run` names two unrelated commands, and `longerCommands` exists to
tell them apart.**
Files: `cmd/aforge/main.go` (the dispatch, `runPlanCommand`, `runPlanNew`,
`runRevise`, `runShow`), `cmd/aforge/run.go` (`runExecute`, `namesAPlanFile`,
`runGraph`), `cmd/aforge/subharness_run.go`, `cmd/aforge/rename.go` (new),
`cmd/aforge/usage.go` (`longerCommands`).
`aforge run <program>` is the one meaning of `run`, matching `/subharness <name>`
in the chat. The static pipeline is `aforge plan new | show | revise | run` — the
noun is **plan**, not `graph`, because a developer plans work, shows the plan,
revises the plan and runs it, and `graph` is how the engine thinks. Today's four
verbs map one-to-one onto the four; no fifth was needed.
`longerCommands` lost its `run subharness` entry and kept `cache clean`, and the
comment says why: `cache` is one noun with two verbs on it, which is a different
shape from one word meaning two things.
Test: `TestAnOldSpellingStillWorksAndSaysWhatItIsCalledNow`,
`TestACommandsUsageIsReadOutOfTheOneTable` (rewritten: `run`'s usage is the
saved-program runner's, and `aforge plan --help` offers all four subcommands).
Capture: `frames/cmd-renames-after.txt`, `frames/cmd-plan-run-help-after.txt`.

**Row 7 — `--budget` means tokens while *budget* means dollars everywhere else.**
Files: `cmd/aforge/exec.go`, `cmd/aforge/run.go`, `cmd/aforge/rename.go`.
The token bound is **`--token-budget`** on `exec` and on `plan run`, and the
whole-run wall is **`--total-token-budget`**. `--budget` and `--run-budget` keep
their old behaviour as hidden aliases for one release. `AFORGE_EXEC_BUDGET` reads
through to the new name, and an old spelling still counts as *typed* so the
variable cannot silently overrule it.
Test: `TestAnOldSpellingStillWorksAndSaysWhatItIsCalledNow`,
`TestApplyExecEnvNeverOverrulesATypedFlag` (extended with the old spelling),
`TestOneConceptIsSpelledOneWayOnEveryDoor`.
Capture: `frames/cmd-renames-after.txt` (`--budget` → the notice on stderr, the
envelope on stdout, `jq -r .answer` reading it).

**Row 8 — `--timeout` is a duration on `do` and an integer of seconds on `exec`.**
Files: `cmd/aforge/exec.go`, `cmd/aforge/wake.go`.
`exec` takes the same `wallFlag` `do` has, so `--timeout 15m` works on both and a
bare number is still seconds. `AFORGE_EXEC_TIMEOUT` reads durations too — it was
a refusal on a machine where the flag it stands in for accepts them. `wake`'s
`--max-seconds` is `--timeout`, hidden for one release.
Test: `TestApplyExecEnvFillsWallsNobodyPassed` (extended: `AFORGE_EXEC_TIMEOUT=2m`),
`TestApplyExecEnvValuesStillMeetTheFlagGuards`,
`TestAnOldSpellingStillWorksAndSaysWhatItIsCalledNow`.
Capture: `frames/cmd-renames-after.txt` (the `--timeout 45s` run).

**Row 11 — `-w`, `-o` and `-j` have no long spelling at all.**
Files: `cmd/aforge/do.go`, `exec.go`, `run.go`, `main.go`, `subharness_run.go`,
`rename.go`.
`--dir`, `--out` and `--parallel` are the printed names. The letters are
**shorthands, not deprecations**: they keep working forever and say nothing,
which is a different lifetime from a rename and therefore a different function
(`shorthandFlag` against `renamedFlag`).
Test: `TestASingleLetterShorthandKeepsWorkingAndSaysNothing`,
`TestNoOldSpellingIsPrintedByHelp`.

**Row 12 — `exec --plan-model` is accepted and documented as doing nothing.**
File: `cmd/aforge/exec.go`. It is off the printed flag list. A script that passes
it keeps running and is told once, on stderr:
`note: exec does not plan — --plan-model has no effect here.`
Test: `TestOneConceptIsSpelledOneWayOnEveryDoor` (it is no longer a printed flag).

**Row 16 — `--yes-spend` is documented as two different things on two commands.**
Files: `cmd/aforge/main.go` (`yesSpendFlagHelp`), `do.go`, `run.go`. One sentence:
*spend past today's limit and past the plan-price question, without stopping to
ask.* `rail` went with the old wording.
Test: `TestOneConceptIsSpelledOneWayOnEveryDoor` (the concept table).

**Row 17 — `--ensemble` is a tri-state integer with two magic values.**
Files: `cmd/aforge/passes.go` (new), `main.go`. `--passes auto|off|<n>`,
defaulting `auto`, refusing `1` by name rather than letting it fall through the
old encoding. `--ensemble` is hidden for one release.
Test: `TestAnOldSpellingStillWorksAndSaysWhatItIsCalledNow`.

**Row 18 — `--contracts` and `--brief` are one concept under two machinery names.**
Files: `cmd/aforge/run.go`, `main.go`, `rename.go` (`invertedFlag`).
`plan run --no-method` (default off, so no `=false` form is needed) and
`plan new --instructions`. `--contracts` is kept as an *inverted* hidden alias,
because `--contracts=false` and `--no-method` are one instruction spelled with
opposite words.
Test: `TestAnOldSpellingStillWorksAndSaysWhatItIsCalledNow`.

**Row 19 — `--context-fill` and `--completion-reserve` document themselves by the
environment variable they set.**
Files: `cmd/aforge/do.go`, `exec.go`. The env-var clause is gone and the figures
are interpolated from `ctxbudget.DefaultFillPercent` and
`ctxbudget.DefaultCompletionReserveTokens`, so the printed default and the real
one are one fact. **The `DefValue` stays 0 deliberately**: zero is how these two
flags say "leave the law alone", and giving them a non-zero default would make
every run set the environment.
Test: `TestOneConceptIsSpelledOneWayOnEveryDoor` (the machinery scan).

**Row 2 — `aforge run --help` says "leaf" or "leaves" six times.**
File: `cmd/aforge/run.go`. Every one is *step*. The scan that keeps it that way
is structural rather than a grep, so it covers flags nobody has written yet.
Test: `TestOneConceptIsSpelledOneWayOnEveryDoor` (the machinery scan bans
`leaf`, `leaves`, `spine`, `seat`, `sheet`, `rail`, `brain`, `charter`,
`verdict`, `errand`, `ensemble`, `contract`, `panel` from any printed flag's help
sentence; `lane` is the ruled exception and `node` is exempted in one place, with
the reason written at the exemption).

**Row 3 — `aforge why <node-id>` help says "show what one leaf actually did".**
File: `cmd/aforge/main.go` (`usageText`). It is `aforge why <task-id>`, "show
what one piece of work actually did". The verb itself is another lane's rename.
Capture: `frames/cmd-help-after.txt`.

**Row 22 — `brain` is the word `doctor` uses for the store.**
Files: `cmd/aforge/doctor.go`, `main.go`. The row is `store`, and `--db`'s help is
one shared constant, `storeFlagHelp` — "the store to work in" — on all seven
doors that had three sentences for it.
Test: `TestDoctorShowsSharedCalmStatusRows` (now forbids `brain` outright),
`TestOneConceptIsSpelledOneWayOnEveryDoor`.
Capture: `frames/cmd-doctor-before.txt` → `frames/cmd-doctor-after.txt`.

**`standing watch` in `doctor` — the coordinator's ruling, not a numbered row.**
File: `cmd/aforge/doctor.go`. The label is **`background timer`**: what the row
measures, in a developer's words, is what is running, since when, and whether it
still answers. `standing watch` is the resident's vocabulary, which a test
forbids the chat's corpus from using, so the manual could not quote doctor's own
output and stay legal — the label moved and the ban stayed. The new label is
quoted on `internal/manual/chat/running-from-the-terminal.md`, in the section
that had to describe the row in other words.
Test: `TestDoctorShowsSharedCalmStatusRows` (forbids `standing watch`),
`TestTheChatManualDoesNotSpeakOfTheResident` (unchanged, and still green).
Capture: `frames/cmd-doctor-before.txt` → `frames/cmd-doctor-after.txt`.

**Rows 1, 23, 24, 25 — the front page.**
File: `cmd/aforge/main.go` (`usageText`, `environmentText`, `usage`).
The opening line is the manual's own sentence — *an agent you talk to, and hand
work to when you walk away* — and the package doc changed with it. The commands
are five headed groups in most-reached-for order with adjacent forms of one verb
together, followed by five worked examples. The environment table is
**`aforge help env`**, and `--help` names that door in its last line.
127 lines → 105, of which none is an environment variable. (105 rather than
COMMANDS.md's estimated sixty: what is left is the command table itself, whose
continuation prose is most of it.)
**The examples are indented FOUR spaces, not two**, and that is load-bearing:
two is what a command row is written with, and `usageForCommand` lifts a
per-command synopsis out of this same table by matching `  aforge ` — so an
example at that indent was printed under `aforge do --help` as though it were
part of `do`'s shape. It was, for one build.
Test: `TestTheHelpPageIsGroupedCommandsAndExamplesAndNotTheEnvironmentTable`,
`TestTheEnvironmentTableHasItsOwnDoor`,
`TestUsageMentionsExecEnvironmentFallbacks` (rewritten to look where the table
now is).
Capture: `frames/cmd-help-before.txt` (127 lines) →
`frames/cmd-help-after.txt` (105) and `frames/cmd-help-env-after.txt`.

**cli-24 — `docs/HEADLESS.md` documents `exec`'s old exit table and old envelope.**
File: `docs/HEADLESS.md`. §1's exit codes, §1's `--json` object, §2's flags,
§2's environment fallbacks, §2's envelope, §2's exit codes, §3's four recipes,
§4's command table and §5's environment are all on the ladder and the envelope
the sibling lane landed, and on the spellings this one landed.
Test: `TestHeadlessDocumentsTheLadderAndTheEnvelopeItActuallyHas` — every field
of the one envelope, the hatch, and a refusal on the old six-code table and on
every retired flag spelling.

### Not fixed here, and why

- **Rows 4, 13, 14, 15, 20, 21, 26, 27, 28, 29** are other lanes' or other
  passes'. Row 4's gate landed earlier in this wave
  (`internal/manual/terminalverbs_test.go`) and the new `aforge plan`
  subcommands are on its page.
- **Row ten (`seat` on stderr)** lives in `internal/config/seats.go`, which is
  outside this lane's files.
- **Row 5 and row 6** are the sibling lane's and were already landed.

### Three stale tests this lane found and fixed

They were red on a clean tree before this change and are not in
`.github/known-red.txt`, so they were caused by earlier commits on this branch
and would have been read as this lane's breakage:

- `TestACommandsUsageIsReadOutOfTheOneTable` and
  `TestExecsExitLadderIsWrittenWhereACallerLooks` asked `usageText` for exec's
  old six-code ladder and for a `do` sentence the envelope lane replaced.
- `TestTheSubharnessFlagIsNotAFlag` read the flag package's own sentence out of
  the returned error, which became an `exitStatus` when a bad flag was put on
  the first rung of the one ladder. The refusal is on stderr; the test reads it
  there now.
- `TestHostFlagIsOnTheChatUsage` looked for `flag: help requested`, the internal
  string that stopped being printed when asking for help stopped being a failure.

`.github/known-red.txt` is unchanged: `TestTickWalksTheItemsAndWritesAWakeLine`
and `TestTickLeavesQuietlyWhenAWindowIsAlreadyKeepingWatch` are the only reds
left in `./cmd/aforge/` and both are on it.

---

## fixed — the streams and the small doors

A second lane, on the rows the rename lane left. Everything below is closed in
`cmd/aforge/` and in `internal/manual/chat/`, each with a named test that was
watched to fail with the fix reverted. The two-stream captures are the evidence
for rows 14, 27 and 28 and nothing else is: they are the same command's `>out`
and `2>err` written to separate files.

**The rule, stated once, and it is now written in three places** — `COMMANDS.md`
§5, `cmd/aforge/streams.go`, and `internal/manual/chat/running-from-the-terminal.md`:

> **stdout is the ANSWER** — the deliverable, the `--json` object, the rows, the
> table, the thing a script captures — **and everything a person reads ABOUT the
> run goes to stderr**: the preamble, the progress, the warning, the receipt
> saying a file was written, the path a record was kept at, and any question the
> command asks. A prompt on stdout is the worst of them, because a script that
> captured the output got a question in its data and the person got a blank
> terminal waiting for a word they could not see.

**Rows 14, 27, 28 — three doors printing their chatter into the answer.**
Files: `cmd/aforge/streams.go` (new — the `aside` writer and the rule),
`cmd/aforge/run.go` (`runGraph`'s preamble, the per-step progress feed, the
`── executing ──` rule, the calibration report, the `written to` receipt),
`cmd/aforge/main.go` (`runPlanNew`, `runRevise`, `runShow`, `emit`),
`cmd/aforge/logs.go`, `cmd/aforge/cache.go`.
`logs`'s `--json` special case disappeared with the move, which is what row 27
predicted: the flag stopped needing to know about a line that was never the
answer.
Test: `TestNoDoorPrintsItsCommentaryToStdout` — **the structural one**. It reads
every non-test file in the package with `go/ast`, finds every write to stdout
(bare `fmt.Print*`, or an `Fprint*` whose writer is `os.Stdout` or one of the
package's answer writers), and fails naming the file, the line and the string
when what is written has the shape of commentary: a **prompt** (ends on a colon
or a question mark with no newline — nothing that is an answer stops mid-line
waiting) or a **label column** (`goal:      `, `models:    ` — a lower-case
label, a colon, and padding that aligns a value). It reads string literals out
of the syntax tree and never the source text, so a comment quoting the old shape
cannot satisfy or fail it, **and it follows `+` concatenation**: the first
version of it went green against the very defect it was written for, because
`cache clean`'s prompt was built out of three operands and only the first was
read.
Also: `TestTheLogsPathHeaderIsAnAsideAndNotTheFirstRow`,
`TestTheCacheQuestionIsAskedOffTheAnswerStream`,
`TestThePlanDoorsKeepTheirPreambleBesideTheAnswerAndNotInIt`.
Capture: `frames/cmd-logs-out-after2.txt` / `cmd-logs-err-after2.txt`,
`frames/cmd-cacheclean-out-after2.txt` / `cmd-cacheclean-err-after2.txt` (and
`cmd-cacheclean-piped-after2.txt`, which is what a person sees when they pipe
it), `frames/cmd-planshow-out-after2.txt` / `cmd-planshow-err-after2.txt`.

**Row 13 — nothing-to-show answered three ways, with no rule.**
Files: `cmd/aforge/services.go`, `cmd/aforge/why.go`; the rule is written in
`COMMANDS.md` §5 and on the manual's terminal page.
The rule: **a listing with nothing in it prints one short sentence saying so, and
a column header is never printed without a row under it.** It is a DIFFERENT
question from the emptiness law and needed its own answer — the emptiness law is
about figures, where zero draws nothing; this is about the sentence, where
nothing is exactly what must not be drawn, because a person who typed a question
and got a blank terminal cannot tell a quiet day from a reader that failed. So:
the figure is absent, the sentence is present.
`aforge services` → `nothing is being kept running.`
`aforge why self` → `nothing was tried on its own account today.`
`aforge cache` had it right all along and is the model. `aforge logs` keeps its
existing arrangement, which the rule now names as the one exception: a filter
that matched nothing prints nothing, because the filter IS the question, while an
id somebody pasted earns a sentence — and `--json` never gets one.
Test: `TestNothingBeingKeptRunningIsASentenceAndNotSilence`,
`TestASelfSpendDayWithNothingOnItSaysSoInsteadOfPrintingAHeader`.
Capture: `frames/cmd-services-out-after2.txt`, `frames/cmd-whyself-out-after2.txt`.

**Row 20 — `devices revoke --all` hand-parsed, position-sensitive, unnamed.**
Files: `cmd/aforge/chatv3_at.go`, `cmd/aforge/main.go` (`usageText`),
`cmd/aforge/usage.go` (`longerCommands`).
`--all` goes through `commandFlags`/`parseCommandFlags`/`reorder` like every other
flag, so both `revoke --all laptop` and `revoke laptop --all` work. `aforge
devices` and `aforge devices revoke <name> [--all]` are now two rows in the one
table — the `cache` / `cache clean` shape — so `devices revoke` joined
`longerCommands` and `aforge devices revoke --help` lifts its own line. `aforge
devices --help` answers instead of refusing with exit 1.
Stopping one device with `--all` answers in `pair.RevokedLine`'s ordinary
sentence rather than a count of one.
Test: `TestDevicesRevokeReadsAllInEitherPositionAndSaysSoInTheUsage`.
Capture: `frames/cmd-devices-revoke-help-after2.txt`.

**Row 21 — two surfaces, two words for the same deletion.**
Files: `cmd/aforge/cache.go` (`cacheCleanWord`), `cmd/aforge/main.go`,
`internal/manual/chat/running-from-the-terminal.md`.
One word, **`now`**, in both, because the chat cannot pass a flag and has always
wanted `/cache clean now`. `--yes` stays as the script's spelling. The word is a
constant and `--help` interpolates it, so the prompt and the page cannot drift.
Test: `TestBothSurfacesConfirmTheCacheDeletionWithTheSameWord` — it checks the
constant against the chat's own manual page, against `usageText`, and that the
OLD word no longer deletes anything.
Capture: `frames/cmd-cacheclean-err-after2.txt`.

**Row 15 — `run` reimplemented the models line.**
File: `cmd/aforge/run.go`. It calls `seats.Report()`, which is the sentence and
the inheritance notice under it, in the one place that owns them. The door's own
label column went with it.
Test: `TestEveryDoorPrintsTheModelsLineThroughTheOneReport` — structural, reading
the package with `go/ast`: any `seats.Sentence()` or `seats.Line()` in `cmd/aforge`
is a door spelling its own version, and the test names the file and line. It also
refuses to pass if nothing calls `Report` at all, so it cannot go quiet.

**Row 29 — `aforge version` printed only `aforge dev`.**
Files: `cmd/aforge/version.go`, `internal/manual/chat/screen.md`.
One line, and it carries everything a defect report is asked for: the revision
and the moment `make build` stamps, then the Go toolchain and the platform from
the runtime. It stays ONE line because an external harness reads it to decide
whether aforge is installed at all.
And a binary that cannot name its source says so rather than wearing the bare
word `dev` like a release name: `aforge dev (no revision stamped — built without
`make build`) · go1.26.5 linux/arm64`. **`debug.ReadBuildInfo` was not the
fallback row 29 assumed it was** — the tree here embeds no `vcs.revision` at all
under a plain `go build`, and there is no `vcs.time` read anywhere, so the
linker stamp is the only real source and the honest thing to do about its
absence is name it.
Test: `TestVersionNamesTheBuildAndTheMachineItWasBuiltFor`. Two smoke tests that
asserted the whole line by equality now assert the stamped revision is its
prefix.
Capture: `frames/cmd-version-after2.txt`.

**Row 26 — `aforge manual --help` printed the page list instead of a usage.**
Files: `cmd/aforge/manual.go`, `internal/manual/chat/running-from-the-terminal.md`.
The usage goes on top, lifted out of the one table like every other door's, and
the list stays under it — nothing is lost and `--help` means one thing on all
twenty-three verbs. The BARE form is still the listing, which is the good half of
the old argument.
Test: `TestManualHelpPrintsTheCommandsUsageAboveThePageList` (both `-h` and
`--help`, and it checks the ORDER, not just that both are present).
Capture: `frames/cmd-manual-help-after2.txt`.

### Rows 1, 23, 25 — already closed before this lane started

`--help`'s opening sentence, its five worked examples and its five headed groups
were landed by the rename lane and are recorded in the section above. Verified
against a fresh build rather than assumed: `frames/cmd-help-after2.txt` opens
`aforge — an agent you talk to, and hand work to when you walk away`, carries the
five groups and the `Examples:` block, and is 108 lines with no environment
variable on it.

### Manual pages changed in the same change

- `running-from-the-terminal.md` — a new `## What goes to stdout and what goes to
  stderr — piping a headless command` section carrying the rule, five copyable
  lines, the three doors that used to break it, and the nothing-to-show sentence
  rule; the `services` and `why self` emptiness sentences; the cache word;
  `aforge manual --help`.
- `reaching-this-machine-without-ssh.md` — `--all` in both positions, both
  sentences it can answer with, and why the old grammar was wrong.
- `screen.md` — what `aforge version` prints, stamped and unstamped.

### Left open, and why

- **Row 10 (`seat` on stderr)** is in `internal/config/seats.go`, outside this
  lane's files.
- **Rows 4, 5, 6** were closed earlier in the wave by other lanes.

---

## fixed — the eighty-column help page, and the fourth exit table

A third lane, on what the rename and streams lanes left. Both fixes below were
checked by reverting them and watching the test fail; what each failure said is
recorded with it, because five tests in this wave passed against the very defect
they named.

**The layout law, stated once and enforced once** — `cmd/aforge/main.go`,
beside `usageText`:

> Nothing this binary prints as help draws wider than **eighty display cells**.
> A command's synopsis begins at column 2 and folds, when it must, to column 14;
> its description sits UNDER it at column 6, never beside it.

**The width (`audit-cli.md`'s row seven, closed in that file's own table) —
`--help` was a designed page laid out to 164 cells, which is not a designed
page on the terminal anybody reads it in.**
Files: `cmd/aforge/main.go` (`usageText`, `environmentText`, the `helpWidth` and
`helpTextColumn` constants and the law above them), `cmd/aforge/envelope.go`
(`exitLadderHelp`, `foldedExitLadder`), `cmd/aforge/usage.go` (the closing line
printed under every per-command page).

The grouping, the five examples and the environment door were already right
(the rename lane, above) and are untouched. What was still wrong was the WIDTH,
and it was wrong everywhere: the front page's longest line drew 164 cells, the
environment table's drew 116, and the one line under all twenty-three
per-command pages drew 83. So every second line was folded by the terminal, at
a break nobody chose, **inside a word** — `--host host[:` / `path]]`,
`instea` / `d of attaching`, `res` / `umes` — and the hanging indent stopped
aligning the moment it happened. `frames/help-before.80x24.txt` is that, folded
the way a terminal folds it.

- **The front page is 108 lines drawing 167 rows → 110 lines drawing 110.** The
  page a person scrolls got a third shorter while gaining nothing they have to
  squint at.
- **The right-hand description column is gone.** It could not survive eighty
  cells: these synopses carry whole flag lists, so the text column started at
  30 on `models` and at 0 on `do`, which is the two conventions the page already
  had. One column, at 6, under the synopsis.
- **The environment table is one column too**, at 23 — it had two (23 and 30),
  and its widest row was an example env assignment nothing could fold. The
  example is two model ids now instead of three.
- **The exit ladder folds ON ITS SEPARATOR.** Five rungs spelled in words is 139
  cells and cannot be one line at any indent, so `exitLadderLine` became
  `exitLadderHelp` — still built from `exitLadder` and from nowhere else, still
  the same text on all three verbs, but folded so that a break is only ever
  taken before a `· `. A rung split across the fold is a rung a reader scanning
  for their own exit code never finds.
- `run `aforge --help` for every command, `aforge help env` for the environment
  table.` — the closing line under every per-command page — drew 83 cells and is
  now `…for the variables.`

Test: **`TestEveryHelpPageFitsAnEightyColumnTerminal`** (`cmd/aforge/helpwidth_test.go`).
It reads the DOORS and not the variables — `usageText` is a concatenation, and
a test measuring the literal would measure a page nobody sees — capturing
`--help`, `help env` and twelve per-command pages, and measuring every line with
`ansi.StringWidth` rather than `len`, which is the same byte-versus-cell law
`wrapAt` already keeps. It reads `cmd/aforge` with `go/parser` to name the FILE
AND LINE the over-wide text was typed at, which is what puts it on
`scripts/laws.sh`.
And **`TestTheHelpPageCostsFewerRowsThanTheOneItReplaced`**, because the obvious
way to make a 164-cell page fit eighty is to print twice as many lines: it
counts rows at 80 and refuses anything that is not fewer than the 167 the old
page drew.
**Reverted and watched fail:** with `usageText`, `environmentText` and the
closing line put back as they were at `HEAD`, the first names twenty-odd lines —
`` `--help` line 24 draws 164 cells in a 80-column terminal, so the terminal
folds it inside a word `` — with `main.go:273`, `:274`, `:276` … beside each, and
fails `doctor --help` and `help env` on their own; the second says
`draws 167 rows … the page it replaced drew 167, so folding it has bought the
reader nothing`.
Capture: `frames/help-before.80x24.txt` → `frames/help-after.80x24.txt`,
`frames/help-env-before.80x24.txt` → `frames/help-env-after.80x24.txt`, each
folded at eighty so the mid-word breaks are visible rather than described.

**The fourth exit table — the chat manual still taught `exec`'s retired
six-rung ladder.**
File: `internal/manual/chat/adaptive-runs.md`.
Row six said there were three exit tables. There were four, and the last one
was the worst: `adaptive-runs` — the page about `aforge exec` — still said *"Its
exit code is a six-rung ladder"* and *"`5` is the one to watch for"*, months
after the binary stopped returning 5 or 6. That corpus is the ONLY authoritative
source about aforge for the model, so the running chat answered "what does exit
5 mean" out of a number the binary no longer publishes. The page now carries the
one ladder, says that **`1` means nothing ran at all** so a run that started and
then fell over leaves with `2`, and names `AFORGE_EXIT_CODES=legacy` for the
script that was pinned to the old numbers. Two smaller stale claims on the same
page went with it: the envelope's answer field is `answer` and not `text`, and
`--plan-model` is kept for an old command line rather than "so that every
command takes the same flags".
Test: **`TestNoChatPageStillTeachesExecsRetiredExitCodes`**
(`internal/manual/execladder_test.go`). It reads `exitLadder` out of
`cmd/aforge/envelope.go` with `go/parser` — the const block for each rung's
NUMBER, the composite literal for its `Short` clause — and demands the page
carry every number and the words `envelope.go` spells it with, plus the hatch;
then it bans the four sentences the retired table was written in, on every page
in the corpus. **The positive half is what discriminates**: a page that merely
deleted its stale paragraph satisfies any ban and leaves the chat with nothing
to say.
**Reverted and watched fail:** with the old page back, both halves fail
independently — three rungs missing (`adaptive-runs does not say what exit 2
means; envelope.go's own words for it are "ran and did not finish"`), the hatch
unnamed, and four bans tripped.
No new `## ` heading was added to the corpus: the fix is inside the section that
already owned the claim, so nothing was put in a position to outrank an
established page on a generic term.

### NOT FIXED — row ten, `seat` on stderr, is not in this lane's files

The notice is spelled in exactly one place, `internal/config/seats.go:331`:

```go
return "your crew was set before the " + string(s.Role) + " seat existed · " +
```

The four headless doors do not spell it themselves — `do.go`, `main.go`,
`exec.go` and `subharness_run.go` all print `seats.Report()`, which is right and
is what row fifteen was closed for. **So the fix is one line in a file this lane may
not touch, and it needs routing.**

Whoever takes it must land three manual edits in the same change, or the corpus
will quote a sentence the binary no longer prints:

- `internal/manual/chat/models-and-cost.md:461` and `:488` quote the notice
  **verbatim** — `your crew was set before the work seat existed · it is running
  on your small work model until you pick a crew again` — and `:480` is a `## `
  heading built on the word (*inherited work seat in the conversation*).
- `internal/manual/chat/models-and-cost.md` uses *seat* as the product's own
  noun about thirteen times (`## What are the six models — the one you talk to
  and the five crew seats`), so this is a vocabulary decision across a page, not
  a string swap. `commands.md:1291` and `:1606` are the two the audit already
  named, and they belong to the same decision.
- `internal/manual/chat/adaptive-runs.md` had one, and it is gone here — the
  sentence did not need the noun at all and now reads *"it opens on the error
  stream naming one model rather than two"*.

`TestEveryPageQuotingAShippedSentenceQuotesItWhole` in `internal/manual` is what
will catch a half-done version of this.

---

## fixed — row 10, and the two stream rows re-verified

A third lane. Row 10 is the only open row here that was this lane's; rows 14 and
28 were checked against a rebuilt binary rather than believed, because a
structural test in this wave once went green against the very defect it named.

**Row 10 — `seat` on stderr on four headless doors.**
File: `internal/config/seats.go` (`Seat.Notice`, and a new `CrewCommand`).

**The row as written is NOT A DEFECT and was declined on the coordinator's
ruling.** `seat` is not machinery vocabulary: it is this product's own noun for
a row of the crew, used about thirteen times on
`internal/manual/chat/models-and-cost.md` including inside a `## ` heading, and
spoken by the settings sheet, the model picker and the `/crew` chooser. The
vocabulary law bans the PROGRAM'S words for its own process — `auditor`,
`verdict`, `refuted` — not a domain noun the product teaches under that name.
Renaming it in this one sentence would have left the corpus quoting a line the
binary no longer printed while the page around it went on saying `seat` a dozen
more times.
The row's other premise was checked and is right: the notice is spelled in
exactly one place and the four doors all print `seats.Report()`, so there is one
function to change and no call site.

**What IS wrong is the remedy, and it is a real defect of the class "a sentence
not true of the code around it".** The line ended `until you pick a crew again`,
and it is printed on four HEADLESS doors — `aforge do`, `aforge plan run`,
`aforge exec`, `aforge run` — where there is no way to pick a crew at all: a crew
is written by `/crew` and by the settings sheet's Providers row, both of which are
the conversation, and no flag and no verb in the binary sets one. The one line
whose whole job is to explain a surprising model choice told somebody to do
something and gave them nowhere to do it. Cause plus what to do is the law on
both surfaces; a cause plus a dead end is the defect.

| | |
| --- | --- |
| before | `your crew was set before the work seat existed · it is running on your small work model until you pick a crew again` |
| after | `your crew was set before the work seat existed · it is running on your small work model until you pick a crew with /crew in the conversation` |

ONE FORM, TRUE FROM BOTH PLACES IT IS PRINTED. `/crew` reads as the next
keystroke in the conversation and as a destination from a shell, and it is the
same sentence in both — which is what keeps somebody who has seen one surface
recognising the other. The door is a constant, `config.CrewCommand`, so a remedy
cannot go on naming a door that has been renamed.
`FromWords` was checked and needs nothing: its one caller is the `/crew`
chooser's own row (`internal/tui3/crew.go`, `your work seat is inherited from
small work — picking one writes it`), whose remedy is the literal next keystroke
where it is drawn. That sentence is not a dead end and is not this lane's file.
Test: `TestTheInheritedSeatNoticeNamesADoorAPersonCanActuallyReach` — the door,
where the door is, and the three register rules the function's own comment sets
out (a middle dot, lowercase, no full stop). **Watched to fail** with the old
promise put back: `the remedy names no door at all, so a person reading it in a
terminal has nowhere to go`. `TestTheNoticeKeepsTheProductsOwnWordForARowOfTheCrew`
is a GUARD on the declined half and passes either way, which is said here because
a test that cannot fail is not evidence.
No test anywhere asserted the old promise — the three files that mention it
(`internal/tui3/crewseat_test.go`, `internal/e2e/tuiwords_test.go`,
`docs/changes/unreleased/503-one-model-status-line.md`) all quote it in prose, and
the e2e needle stops at `it is running on your ` on purpose. `./internal/config`,
`./internal/e2e`, `./internal/manual` and the `/crew` tests in `./internal/tui3`
are green.

**NEEDS ROUTING: the manual quotes the old sentence three times** and
`internal/manual/` is another lane's this wave. `models-and-cost.md:461` and
`:488` print the line inside fenced blocks and `:779` quotes its tail; all three
need `until you pick a crew again` → `until you pick a crew with /crew in the
conversation`. The prose around them already says `/crew` is how you end it, so
nothing else on the page moves. No manual gate fails today — the corpus is simply
quoting a line the binary no longer prints, which is the thing the manual law
exists to prevent.

**Row 14 — the plan doors' preamble.** Re-verified, not re-fixed. `main.go`'s
`runPlanNew`/`runRevise`/`runShow` and `run.go`'s `runGraph` all write through
`aside`; `aforge plan show /nope/p.json > out 2> err` leaves stdout **empty** and
the whole refusal on stderr. **This was verified by capture and by reading the
source, and NOT by a revert check**: both files are held by other lanes this wave
and a temporary edit to `main.go` in a shared checkout would have raced them.

**Row 28 — `cache clean`'s prompt.** Re-verified by revert, which is the one that
mattered: this is the row whose structural test was once green against it because
it read only the first operand of a three-part concatenation. Putting the four
prompt lines back on `os.Stdout` makes
`TestNoDoorPrintsItsCommentaryToStdout` fail naming `cache.go:102` and the
concatenated string — `writes a question to STDOUT: "Type \"\x00\" to delete it;
anything else keeps it: "` — so it now follows `+` and now fails for the right
reason. `TestTheCacheQuestionIsAskedOffTheAnswerStream` fails beside it.
Live, on the rebuilt binary with a non-empty cache: stdout carries only `kept —
nothing was deleted.` and the four question lines are all on stderr.

## fixed — verified rather than re-done

**Rows 4, 5 and 6** were closed by earlier lanes in this wave and never written
down here, so the ledger went on calling three high rows open. Verified in the
tree rather than taken on trust:

- **Row 4** — all six verbs (`why`, `notebook`, `competence`, `services`,
  `wake`, `rebuild`) are in `internal/manual/chat/running-from-the-terminal.md`,
  and `TestTheChatManualMentionsEveryVerbTheCommandLineAnswersTo` fails the
  build if a verb the command line answers to has no page.
- **Row 5** — there is ONE envelope. `buildExecEnvelope` fills `runResult` and
  hands it to `buildResultEnvelope`, which is the only place a `resultEnvelope`
  is constructed.
- **Row 6** — the three tables the row named are gone, a fourth was found in the
  chat manual and closed by `dc1a3a898`, and **a FIFTH turned up in
  `docs/TRAJECTORIES.md`** — a per-verb list (`exec` 0/2/3/4/5/6) stale in both
  its rungs and its line numbers. That document now names `exitLadder` as the
  one ladder and deliberately restates no numbers.
