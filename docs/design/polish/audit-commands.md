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
