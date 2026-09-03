# One result envelope and one exit ladder

`aforge do`, `aforge exec` and `aforge run subharness` are the three headless verbs.
Before this change they had **three exit-code tables and two `--json` shapes**, written
where each command was, and nothing anywhere put them side by side. Two of the tables
meant opposite things by the same number. This puts all three on one table and one
object, both defined in `cmd/aforge/envelope.go` and read from nowhere else.

**This changes what existing scripts see.** Everything below is the whole of what moved.

---

## The paragraph to read if you have a script in production

Exit codes moved on all three commands. `aforge exec` moved the most: it used to return
2, 3, 4, 5 and 6 and never 1, and now it returns 0, 1, 2 and 3 like everything else —
`AFORGE_EXIT_CODES=legacy` puts its old numbers back **for one release** and changes
nothing else, so a script can keep running today and be fixed on its own clock. `aforge
do` splits its old 1 into 1, 2, 3 and 4 and its old 2 into 2 and 3; `aforge run
subharness` keeps 0, 1 and 2 with the meanings it already had. **No `--json` field was
removed or renamed away**: every old field name is still printed, beside its new
spelling, for one release. The one thing that genuinely broke is a caller that tested
for the *presence* of `do`'s `error` key to detect failure — `error` is now always
present and empty on success, so test its value, or read `ok`. The field to move a
script to is `stop`, which names why a run ended in a word, is the same word on all
three commands, and is not going to move again.

---

## Exit codes, before and after

The ladder, as it now stands for all three verbs:

| code | meaning |
| --- | --- |
| 0 | it is done, and what is on stdout is the answer |
| 1 | it could not be run at all — no key, bad arguments, the store would not open |
| 2 | it ran and did not finish: part of the work does not stand |
| 3 | a limit you set stopped it — the wall, the token budget, the turn cap, the price |
| 4 | it needs an answer from you and nobody was there |

### `aforge do`

| what happened | before | after |
| --- | --- | --- |
| the deliverable stands | 0 | 0 |
| the store would not open, the workspace could not be made, no resident took it | 1 | **1** |
| the job failed or was cancelled | 1 | **2** |
| the delivery did not land whole (gate stood by its rejection, parts failed) | 2 | 2 |
| a refusal that was not a question | 1 | **2** |
| the plan crossed the consent threshold and `--yes-spend` was not given | 1 | **3** |
| the wall (`--timeout`) arrived | 2 | **3** |
| it stopped to ask, and nobody was there | 1 | **4** |
| the wall arrived with a question standing behind it | 2 | **4** |

### `aforge exec`

| what happened | before | after | under `AFORGE_EXIT_CODES=legacy` |
| --- | --- | --- | --- |
| the model answered | 0 | 0 | 0 |
| the token budget ran out | 2 | **3** | 2 |
| the turn cap ran out | 3 | **3** | 3 |
| the wall arrived | 4 | **3** | 4 |
| the provider failed, the key was missing, the model id was rejected | 5 | **1** | 5 |
| it finished with nothing to show | 6 | **2** | 6 |
| any other ending (cancelled, paused, promoted, split) | 5 | **2** | 5 |

`exec` never returned 1 before, and 1 is what every other command in the binary returns
for "could not be run at all". That is the whole reason its numbers moved.

### `aforge run subharness`

| what happened | before | after |
| --- | --- | --- |
| it finished and produced its shape | 0 | 0 |
| the name is not a program, the bundle would not load, there was nothing to fall back on | 1 | 1 |
| it ran and did not finish | 2 | 2 |

`run subharness` did not move. It was already the shape the other two were pulled onto.

---

## `--json`, before and after

One object, on stdout, always parseable, **printed even when the run failed**:

```json
{
  "ok": true,
  "stop": "done",
  "answer": "…",
  "files": ["notes.md"],
  "error": "",
  "spend_usd": 0.0213,
  "tokens": {"in": 18422, "out": 1130},
  "seconds": 91.4,
  "model": "anthropic/claude-opus-4",
  "steps": 3
}
```

| old name | on | new name | note |
| --- | --- | --- | --- |
| `deliverable` | `do` | `answer` | both printed for one release |
| `text` | `exec` | `answer` | both printed for one release |
| `artifacts` | `do`, `exec` | `files` | both printed for one release; never null |
| `spend` | `do` | `spend_usd` | both printed for one release |
| `nodes` | `do` | `steps` | both printed for one release |
| `turns` | `exec` | `steps` | both printed for one release |
| `elapsed_ms` | `exec` | `seconds` | both printed for one release |
| `usage` | `exec` | `tokens` + `spend_usd` | both printed for one release |
| `stop` | `exec` | `stop` | same field, wider vocabulary — see below |
| `settled` | `do` | — | **kept, not renamed.** See below |
| `error` | `do`, `exec` | `error` | same field, now always present |
| — | all three | `ok` | new |
| `model` | `do` | `model` | unchanged; `exec` and `run` did not report it before and now do |
| — | all three | `spend_usd`, `tokens`, `seconds`, `steps` | `exec` and `run` gain what only `do` reported, and the reverse |

Fields that belong to one verb and stay: `do`'s `spend_work`, `spend_overhead`,
`blocked_on`, `learned`, `plan_model`, `model_source`, `plan_model_source`,
`subharness`; `run`'s `output`, `report`, `incomplete`.

### `stop` is the field to read

| `stop` | meaning | exit |
| --- | --- | --- |
| `done` | the work is finished | 0 |
| `error` | it could not be run at all | 1 |
| `incomplete` | it ran and part of it does not stand | 2 |
| `budget` | the token budget ran out | 3 |
| `turn-cap` | the turn cap ran out | 3 |
| `deadline` | the wall arrived | 3 |
| `price` | the price crossed the consent threshold | 3 |
| `question` | it needed an answer and nobody was there | 4 |

`exec`'s five existing values — `done`, `budget`, `turn-cap`, `deadline`, `error` — are
spelled exactly as they were. Endings `exec` reports that are not rungs of their own
(`cancelled`, `paused`, `promote`, `split`, `empty`, `overrun`) still pass through under
their own names and land on exit 2.

**One value moved.** `exec` used to report `"stop": "done"` for a run that finished
having produced no text at all, and exit 6 under it. That now reports
`"stop": "incomplete"` and exit 2. The old value was actively misleading — it said the
work was done about a run with nothing to show — so it was not preserved.

### Two things that are deliberately not what COMMANDS.md says

1. **`settled` does not become `ok`.** COMMANDS.md §5 calls it a rename. It cannot be
   one: `settled` means "nothing this run is waiting for can still move", which is
   *true* of a run that asked a question and did nothing — `settled: true` under a
   non-zero exit. A caller that read the new name with the old meaning would record
   every refusal as a success. So `ok` is a new field meaning "the work stands" (true on
   exactly the runs that exit 0), and `settled` keeps its own meaning in its own field,
   unchanged and not deprecated.

2. **`error` is now always present.** COMMANDS.md's envelope shows `"error": ""` and its
   guarantee is "`error` non-empty means the run did not start", which requires the key
   to be there. On `do` and `exec` it used to be `omitempty`. A caller testing for the
   key's presence rather than its value now sees failure on every run. This is the one
   old reader this change genuinely breaks, and it is called out in the paragraph at the
   top for that reason.

---

## The escape hatch

```
AFORGE_EXIT_CODES=legacy aforge exec "…"
```

Restores `aforge exec`'s old 2/3/4/5/6 **for one release** and changes nothing else: not
`do`, not `run`, not one field of the envelope, not one word on stderr. It is one line
of code (`legacyExitCodes`, `cmd/aforge/envelope.go`), one line in `aforge --help`'s
environment table, and one section in the manual. It is not a general compatibility mode
and must not become one.

---

## Where it lives, and what holds it

- `cmd/aforge/envelope.go` — the ladder (`exitLadder`), the stop vocabulary, the
  envelope, the one builder (`buildResultEnvelope`), the hatch, and the per-verb old
  spellings. Nothing else in the binary writes an exit number or an envelope field.
- `cmd/aforge/envelope_test.go`:
  - `TestTheExitLadderIsOneTable` — every rung, its number, the condition that produces
    it, and each of the three verbs' own endings mapped onto it. A change to any verb
    that disagrees fails here by the name of the row it broke.
  - `TestTheThreeVerbsReturnOneEnvelope` — a success and a failure on each of `do`,
    `exec` and `run`, asserting the same keys from the same builder, that the old
    spellings are still there, and that `ok` is not `settled`.
  - `TestLegacyExitCodesRestoresExecsOldRungsAndNothingElse` — the hatch restores exactly
    the old numbers and touches nothing else.
- `cmd/aforge/exec_test.go` — `TestExecLegacyExitCodeIsTheOldTable` pins the old table
  itself, so the hatch cannot quietly stop being the old numbers.
- `internal/manual/chat/running-from-the-terminal.md` — the exit codes, the envelope, the
  old field names, and the hatch, as the chat answers them.
- `internal/manual/chat/saved-programs.md` — `run subharness`'s endings, now the same
  numbers as everything else, and its `--json`.

- `internal/config/settings.go` — `AFORGE_EXIT_CODES` is registered in
  `OperatorEnvPins` (the read-only environment footer), not as a settings row. The
  argument is in the comment beside it and is the one already written three times in that
  file for `AFORGE_SWARM`, `AFORGE_SPLITGATE` and `AFORGE_GROWTH_GATE`: a hatch that lives
  for one release and then goes has exactly the lifetime a persisted setting must not
  have — and a person who once chose `legacy` in a preference sheet would have their exit
  codes silently rolled back on a machine where the variable is nowhere in sight, which is
  the failure the hatch exists to prevent.

Captured from real binaries: `frames/cmd-envelope-before.txt` and
`frames/cmd-envelope-after.txt`, same commands, same demo home, one built from `HEAD` and
one from this branch. The `do` and `exec` objects print their keys in alphabetical order
now rather than in declaration order, because the old spellings are merged in beside the
contract; JSON objects are unordered and nothing reads them positionally.

---

## For the change entry

`invalidates:` lines this change needs, written as statements somebody now believes
wrongly:

- `aforge exec` exits 2/3/4/5/6 and never 1 — it now uses the same 0/1/2/3 ladder as
  every other headless verb, and `AFORGE_EXIT_CODES=legacy` restores the old numbers for
  one release.
- `aforge do` exit 1 means "nothing usable came back" and exit 2 means "partial" — 1 now
  means only "it could not be run at all", 2 is "it ran and part of it does not stand", 3
  is "a limit you set stopped it" (the wall, the price) and 4 is "it needed an answer and
  nobody was there".
- `do --json` and `exec --json` are two different objects — they are one, with `ok`,
  `stop`, `answer`, `files`, `error`, `spend_usd`, `tokens`, `seconds`, `model`, `steps`;
  the old field names are printed beside the new ones for one release.
- `aforge run subharness` has no `--json` — it does, and it prints the same object.
- `do --json` omits `error` on a run that worked — it is always present, and empty.

---

## The rename, before and after — `run` stopped meaning two things

**This landed after the envelope above, in the same wave, and it moves what a script
TYPES rather than what it reads.** It is written here rather than only in
`audit-commands.md` because a person with a harness in production needs both halves in
one place: the numbers and fields changed under them, and so did four of the words.

`aforge run` used to be two unrelated commands wearing one verb — `aforge run
<graph.json>` drove the static pipeline, `aforge run subharness <name>` ran a saved
program — and the code admitted it: `cmd/aforge/usage.go` carried a `longerCommands` table
whose only job was to stop `aforge run --help` printing the wrong synopsis. `run` means
the saved program now, matching `/subharness <name>` in the chat, and the pipeline moved
under **`plan`**, the noun its four verbs all act on.

### The verbs

| what you used to type | what it is now |
| --- | --- |
| `aforge run subharness <name> --input …` | `aforge run <name> --input …` |
| `aforge run <graph.json>` | `aforge plan run <plan.json>` |
| `aforge plan "<goal>"` | `aforge plan new "<goal>"` |
| `aforge show <graph.json>` | `aforge plan show <plan.json>` |
| `aforge revise <graph.json> "…"` | `aforge plan revise <plan.json> "…"` |

Today's four pipeline verbs mapped one-to-one onto `new`, `show`, `revise` and `run`, so
no fifth was needed. `longerCommands` kept only `cache clean` — one noun with two verbs on
it is a different shape from one word meaning two things.

### The flags

| what you used to type | on | what it is now | why |
| --- | --- | --- | --- |
| `--budget <n>` | `exec`, `plan run` | `--token-budget <n>` | *budget* is a word about **money** everywhere else here — `AFORGE_DAILY_BUDGET`, `/budget`, `--max-cost` — so `--budget 150000` read as $150,000 |
| `--run-budget <n>` | `plan run` | `--total-token-budget <n>` | the same, for the whole-run wall |
| `--turns <n>` | `exec`, `plan run` | `--max-turns <n>` | it is a limit, and every other limit says so |
| `--max-seconds <n>` | `wake` | `--timeout <duration>` | one duration flag on `do`, `exec`, `plan run` and `wake` |
| `--timeout <seconds>` | `exec` | `--timeout <duration>` | same name, and now the same TYPE as `do`'s. A bare number is still seconds |
| `--brief` | `plan new` | `--instructions` | it writes a self-contained instruction for every step |
| `--contracts=false` | `plan run` | `--no-method` | a boolean that defaults on needs a negative spelling |
| `--ensemble 0\|-1\|N` | `plan new` | `--passes auto\|off\|N` | a tri-state is words, not magic integers |
| `--plan-model` | `exec` | — | it never did anything; it is still parsed and now says so |
| `-w`, `-o`, `-j` | everywhere | `--dir`, `--out`, `--parallel` | the letters are **shorthands and keep working forever**, silently |

`--db`'s help is one sentence on all seven doors that take it — "the store to work in" —
and `aforge doctor`'s first row is labelled `store` rather than `brain`. Its third row is
`background timer` rather than `standing watch`.

### What a script sees

**Every old spelling still works for one release**, does exactly what it always did, and is
absent from `--help`. Each one prints **one line, on stderr**, the first time it is used:

```
note: `aforge run subharness <name>` is now `aforge run <name>` — the old spelling works for one more release.
note: `--budget` is now `--token-budget` — the old spelling works for one more release.
```

**The notice is never on stdout**, so an old spelling beside `--json` still hands `jq` a
parseable object — captured from a real binary in
`frames/cmd-renames-after.txt`. A single-letter shorthand prints nothing at all, because it
is not going away; that is the difference between `shorthandFlag` and `renamedFlag` in
`cmd/aforge/rename.go`.

`aforge run <something>` tells its two old meanings apart by what was named: a first
positional **spelled as a path** — a separator in it, a leading `./`, `../` or `~`, or a
file extension — is the pipeline spelling, and a bare word is a program. It is the shape of
the argument and never the contents of the working directory: reading it off `os.Stat` made
`aforge run formatter` mean the saved program in one folder and `./formatter` as a static
plan in the next. The positional is found through the union of both doors' flag sets
(`namesAPlanPath`), so `aforge run myprogram --input in.json` is not confused by the input
file named beside it.

An old flag spelling **counts as typed**: `aforge exec --budget 9000` is a decision, and
`AFORGE_EXEC_BUDGET` does not overrule it — which is the same law the environment
fallbacks were guarded by all along, read through the aliases.

### Where it lives, and what holds it

- `cmd/aforge/rename.go` — the notice, its writer, and the two kinds of hidden flag. The
  mark is carried in the flag's own (never printed) usage string rather than in a map keyed
  by flag set, so there is no state to clean up.
- `cmd/aforge/passes.go` — `--passes auto|off|<n>`.
- `cmd/aforge/vocabulary_test.go`:
  - `TestAnOldSpellingStillWorksAndSaysWhatItIsCalledNow` — thirteen old spellings, each
    routed, each saying ONE line naming the new spelling and the release it goes in.
  - `TestNoOldSpellingIsPrintedByHelp` — the front page, the environment page and seven
    per-command pages, read as one body of text.
  - `TestASingleLetterShorthandKeepsWorkingAndSaysNothing`.
  - `TestARenameNoticeNeverReachesTheJSONOnStdout` — with `--json` actually on, asserting
    stdout parses and holds not one word of the notice.
  - `TestOneConceptIsSpelledOneWayOnEveryDoor` — reads every flag declaration in
    `cmd/aforge` with `go/ast`: no retired spelling is a printed flag, no machinery word
    reaches a printed help sentence, and the four concepts that span doors reach for one
    shared constant. That import is what puts it on the pull-request gate.
  - `TestTheHelpPageIsGroupedCommandsAndExamplesAndNotTheEnvironmentTable`,
    `TestTheEnvironmentTableHasItsOwnDoor`.
  - `TestHeadlessDocumentsTheLadderAndTheEnvelopeItActuallyHas` — `docs/HEADLESS.md` on
    the envelope and the ladder above, and refusing the old six-code table.
- `internal/manual/chat/running-from-the-terminal.md` — two new sections, *The old
  spellings* and *Which flags moved*, plus the plan pipeline and `doctor`'s new labels.
  `saved-programs.md`, `adaptive-runs.md`, `models-and-cost.md`, `lanes.md` and
  `commands.md` carry the same words.

### For the change entry

`invalidates:` lines, written as statements somebody now believes wrongly:

- `aforge run <graph.json>` runs a task graph and `aforge run subharness <name>` runs a
  saved program — `aforge run <program>` is the only meaning of `run` now, and the
  pipeline is `aforge plan new | show | revise | run`. Both old spellings work for one
  release and say so on stderr.
- `aforge plan`, `aforge show` and `aforge revise` are top-level commands — they are
  `aforge plan new`, `aforge plan show` and `aforge plan revise`; the bare spellings work
  for one release.
- `--budget` and `--turns` are how `aforge exec` and the plan runner take their token and
  turn walls — they are `--token-budget` and `--max-turns`; `--run-budget` is
  `--total-token-budget`.
- `aforge exec --timeout` takes an integer of seconds — it takes a duration, like `do`'s,
  and a bare number is still seconds. `AFORGE_EXEC_TIMEOUT` reads durations too.
- `aforge wake --max-seconds` is the wall — it is `--timeout`, and it takes a duration.
- `aforge plan --ensemble 0|-1|N` and `aforge run --contracts=false` — they are
  `--passes auto|off|<n>` and `--no-method`.
- `aforge exec --plan-model` is accepted for parity — it is off the flag list, still
  parsed, and says `exec does not plan — --plan-model has no effect here`.
- `-w`, `-o` and `-j` are the only spellings — `--dir`, `--out` and `--parallel` are what
  `--help` prints; the letters keep working forever.
- `aforge --help` prints the environment table — it prints five headed groups and five
  examples; the table is `aforge help env`.
- `aforge doctor` prints a `brain` row and a `standing watch` row — they are `store` and
  `background timer`.
- `docs/HEADLESS.md` documents `exec`'s 2/3/4/5/6 and a `text`/`elapsed_ms`/`usage`
  object — it documents the one ladder and the one envelope.

---

## Not in this change

- The verb renames `COMMANDS.md` §7 ranks second — `aforge why self` → `aforge spend` and
  `aforge why <node-id>` → `aforge tasks <id>` — are a separate row and a separate lane.
  `why`'s help line lost `leaf` and `<node-id>` here; the verb itself did not move.
- `seat` reaching a person on stderr (audit row 10) lives in `internal/config/seats.go`,
  outside the rename lane's files.
- `plan run`'s preamble going to stdout where `do`'s goes to stderr (audit row 14) and
  its second copy of the models line (row 15) are rows of their own.
