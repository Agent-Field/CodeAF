# A developer met this program for the first time today

Seven findings from someone using `aforge do` on `dev` at `b95f8c29`, with no
part in this wave and nothing invested in how it currently works. That makes
these the most valuable rows in the ledger: every other audit here was written
by somebody who already knew the answer.

Each was checked against THIS BRANCH before being written down, because three of
them this wave has already fixed and a row that reopens closed work wastes the
next lane's day. Where my reading differs from the report, the row says so.

1. The crew banner contradicts itself in two consecutive lines — `work deepseek/deepseek-v4-pro` and then `your crew was set before the work seat existed · it is running on your small work model until you pick a crew again`, which calls v4-pro small. A developer cannot tell which model the lane is actually on, and the two sentences are written by two different places that have never been read together. `internal/config/seats.go` (`Seat.Notice`, `tierWords`) against whatever prints the banner above it. THE SAME FUNCTION AS THE ROW BELOW ABOUT THE REMEDY — one fix, not two. — sev: high — frames: n/a (headless)
2. Global help calls `OPENROUTER_API_KEY` **required** and it is not — the developer's run started with no such variable, because the key lives in `config.json`. STILL TRUE ON THIS BRANCH: `aforge help env` line 7. A required thing that is not required sends somebody to find a key they already have, and it is the first table on the front door. — sev: high — frames: `help-env-after.80x24.txt`
3. `--debug` promises a record `in a folder of its own under the state root` and the developer could not find it — `~/.aforge/runs/aforge-do-<n>/` holds only graph scratch. STILL TRUE ON THIS BRANCH (`do --help`, the `--debug` entry). Either the sentence names the wrong place or the folder is not being written; find out which before writing either. A promise about where evidence lives is the one sentence a person reads while trying to file a bug. — sev: high — frames: n/a
4. `do` has no `--yolo`, and a developer coming from the chat types it and gets `error: flag provided but not defined: -yolo` and exit 1. `do` IS unattended by construction, so the flag would be meaningless — but the manual and the chat both use `--yolo` for that posture, so the vocabulary teaches a word the door refuses. Say in `do --help` what unattended means here and whether `--yes-spend` is its equivalent. — sev: med — frames: n/a
5. `do --json` carries spend, nodes, seconds, settled and model but no CALL COUNT and no ROUND COUNT, so the developer went to `~/.aforge/logs/calls.jsonl` to reconstruct them — and its start rows carry no run id, so attribution there is by timestamp alone. Two halves, and the second is the one that bites: a log a person is driven to read should be joinable to the run that wrote it. — sev: med — frames: n/a
6. A refused flag is echoed back in a spelling the person did not type: `--nosuchflag` comes back as `error: flag provided but not defined: -nosuchflag`. Go's flag package writes it, and every other door on this surface now spells flags with two dashes. Somebody scanning for their own typo is looking for a string that is not there. — sev: low — frames: n/a
7. NOT A DEFECT ON THIS BRANCH, and recorded so nobody reopens it: the report says a bad flag and a failed run both exit 1, so a typo and a failure are indistinguishable to a script. That was TRUE ON DEV and is FALSE HERE. `7d6e372c6` moved `exec.StopError` off exit 1 — an outcome only exists once the run has started — so a run that started and failed is now exit 2 `ran and did not finish`, and exit 1 means no outcome at all. A TYPO IS EXACTLY THAT, and the ladder's published words for exit 1 name `bad arguments` outright. I am NOT adding a usage rung: the two endings a script must tell apart are "fix your invocation" and "the work did not stand", and they are now 1 and 2. What remains of this finding is row 6. — sev: n/a — frames: n/a
8. NOT A DEFECT ON THIS BRANCH: the report says the usage banner prints `--db` and `--timeout 900` while `do --help` prints single-dash, and that the banner still shows the bare-seconds form. Both were fixed here — `dc1a3a898` and the rename lane's `commandFlags` — and the banner and the help page are now ONE STRING. Verified by running the binary: `aforge do --nosuchflag` prints the same double-dash synopsis as `aforge do --help`, with `--timeout 15m`. — sev: n/a — frames: n/a

---

## fixed

Landed on `ui/polish-v0`. Every row was re-run against a rebuilt `bin/aforge` and the
capture saved beside the old one under `docs/design/polish/frames/`. **Every test below
was checked by reverting its fix and watching it fail**, except the two marked GUARD,
which pin the other direction and say so.

**Row 1 — the crew banner contradicted itself.** `internal/config/seats.go`
(`Seat.Notice`), `internal/e2e/tuiwords_test.go` (the needle for the promise half),
`internal/manual/chat/models-and-cost.md` (three quotations). Test:
`TestTheInheritedNoticeNamesTheSeatItBorrowedFromRatherThanDescribingTheModel` in
`internal/config/seats_test.go`.

The two lines were never in disagreement about the *fact*. When a seat's source is
`SeatInherited` the model on the models line **is** the inherited one — always, because
that is what inheriting means. What differed was the grammar: `small work` is the NAME OF A
SEAT on the settings sheet, and putting it in front of `model` turns a seat's name into an
adjective about the model it holds. So the notice never characterises the model a second
time; it says which SEAT lent it. Any description was going to contradict the line above,
since the two are one model.

| | |
| --- | --- |
| before | `models: work deepseek/deepseek-v4-pro (crew custom, inherited)` / `your crew was set before the work seat existed · it is running on your small work model until you pick a crew with /crew in the conversation` |
| after | `models: work deepseek/deepseek-v4-pro (crew custom, inherited)` / `your crew was set before the work seat existed · it is running on your small work **seat's** model until you pick a crew with /crew in the conversation` |

`frames/dev-D1-before.txt` → `frames/dev-D1-after.txt`, both from the real binary on a
profile holding only `models.tiers.low`.

**Row 2 — `OPENROUTER_API_KEY` is not required.** `cmd/aforge/main.go`
(`environmentText`), `internal/manual/chat/running-from-the-terminal.md`. Test:
`TestTheEnvironmentPageDoesNotCallTheKeyVariableRequired` in `cmd/aforge/doctor_test.go`,
which builds a profile holding a key, clears both variables, asserts through
`config.APIKeyAt` that such a run really does start, and then reads the row.

What is required is A KEY, from any one of three rungs — the ladder `config.APIKeyAt`
climbs and `doctor`'s key row reports: `OPENROUTER_API_KEY`, then `OPENAI_API_KEY`, then
`api_key` in the profile's `config.json`, which is where the first-run paste and
`/settings` put it. The row names all three and points at `aforge doctor` for which one
answered, matching `key set · <where>`. `frames/dev-D2-before.txt` →
`frames/dev-D2-after.txt`, and `frames/help-env-after.80x24.txt` regenerated.

**Row 3 — `--debug` named the wrong place; the folder is real.** `cmd/aforge/main.go`
(new `debugFlagHelp()` and `debugRecordRoot()`), `cmd/aforge/do.go`, `exec.go`,
`chatv3.go`. Test: `TestTheDebugFlagNamesTheFolderTheRecordIsActuallyWrittenTo` in
`cmd/aforge/debugrecord_test.go`, which opens a real record and asserts the help names the
folder that appeared.

**The finding: the FOLDER was right and the SENTENCE was wrong.** `--debug` writes
`~/.aforge/logs/trace/<run>/`, beside the model-call log, and the run announces the exact
path on stderr (`debug record: …`). The developer went to `~/.aforge/runs/aforge-do-<n>/`
because the state root has two folders and the sentence named neither — and because the
line directly above says `record kept at …` about a *different* thing, the graph scratch
`--keep` holds. The help now names the real root, interpolated from
`trace.DirName`/`trace.TraceDirName` through `internal/home` so it follows `AFORGE_HOME`,
and is one sentence on all three doors instead of three copies.

| | |
| --- | --- |
| before | `…in a folder of its own under the state root (env AFORGE_DEBUG)` |
| after | `…in a folder of its own under ~/.aforge/logs/trace (env AFORGE_DEBUG)` |

**Row 4 — `do` has no `--yolo`, and now says so.** `cmd/aforge/usage.go` (`flagRefusal`,
`flagInstead`), `cmd/aforge/main.go` (`handWorkFooter`),
`internal/manual/chat/running-from-the-terminal.md`. Test:
`TestTheDoorThatHasNoYoloSaysWhatToTypeInstead` in `cmd/aforge/usage_test.go`.

The flag stays absent — a capability that cannot work is absent, not broken — but the
refusal names the near miss, and the page all three verbs share says what unattended means
here before anybody has to trip over it. `--yes-spend` IS the equivalent on `do` and `run`;
`exec` has no such flag and the refusal says what bounds it instead rather than sending
somebody after one.

| | |
| --- | --- |
| before | `error: flag provided but not defined: -yolo` |
| after | `error: aforge do has no --yolo flag — nothing here stops to ask, and --yes-spend answers the one question a run can still stop on` |

`frames/dev-D4-before.txt` → `frames/dev-D4-after.txt`.

**Row 5 — the call count, the round count, and the run id.** `cmd/aforge/envelope.go`
(three contract fields), `cmd/aforge/do.go` (`errandRounds`, the run carried from the
door), `cmd/aforge/exec.go`, `internal/calllog/calllog.go` (`Record.Run`, `CallsFor`),
`internal/provider/calllog.go` (the writer), `internal/manual/chat/running-from-the-terminal.md`,
`docs/HEADLESS.md`. Tests: `TestBothRowsOfACallNameTheRunThatMadeIt`
(`internal/provider/calllog_test.go`), `TestAnErrandCountsTheRoundsItsOwnJobsBought`
(`cmd/aforge/do_test.go`), `TestTheEnvelopeNamesItsRunAndCountsItsCallsAndRounds`
(`cmd/aforge/envelope_test.go`, GUARD — see below).

**Three fields, all always present**, on the terms `steps` already publishes — a zero is a
measurement nobody took and never a count of none:

| field | what it holds |
| --- | --- |
| `run` | this invocation's id (`internal/trace`). It names the `--debug` folder and every row this run wrote into `calls.jsonl`, so `aforge logs --run <id>` joins the two |
| `calls` | model calls this run made, counted at the one door they all pass through, whether or not the log file is on |
| `rounds` | growth decisions journaled against this run's own jobs — how many times it went back for more work. `exec` and a saved program report `0` |

The half that bit is the second one, and it turned out `aforge logs --run` had been reading
a `run` key off those rows for as long as it has existed while **nothing wrote one**. Both
rows of a pair carry it now, not only the start: the end row holds the cost and the finish
reason, and a reader filtering to one run must not have to pair every row first.

**Row 6 — a refused flag is spelled the way it was typed.** `cmd/aforge/usage.go`
(`flagRefusal`), and `cmd/aforge/usage_test.go`, `subharness_test.go` updated where they
pinned Go's own sentence. Test: `TestARefusedFlagIsSpelledTheWayItWasTyped`.

| | |
| --- | --- |
| before | `error: flag provided but not defined: -nosuchflag` / `error: invalid value "notanumber" for flag -turns: parse error` |
| after | `error: aforge do has no --nosuchflag flag` / `error: invalid value "notanumber" for flag --turns: a whole number of turns to allow, such as 200` |

`frames/dev-D6-before.txt` → `frames/dev-D6-after.txt`.

**Rows seven and eight were NOT reopened**, exactly as the ledger asks. (Spelled as words
because `scripts/ledger.py` reads `Row <digit>` under `## fixed` as a closure, and those two
are recorded as NOT DEFECTS rather than as work anybody did.)

### the two guards, said plainly

`TestTheEnvelopeNamesItsRunAndCountsItsCallsAndRounds` is a GUARD on the three keys being
present: removing the fields is a compile error rather than a red test, so there is no
revert that makes it fail gracefully. Its second half — the values — is a real assertion
and does fail when the mapping in `buildResultEnvelope` is broken.

`TestNoDoorPrintsItsCommentaryToStdout` was checked BOTH ways for row 28 (see
`audit-cli.md`) and is not a guard.
