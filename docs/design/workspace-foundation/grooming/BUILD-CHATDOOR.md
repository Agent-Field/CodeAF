# The chat door onto ongoing work

2026-09-11. Lane `codex/personal-chatdoor` (Claude Code Opus on Spark), base
`caa0c5bb7` on `codex/personal-ai-backend`. Round 1: `b06934cf3`, `4ae907108`,
`178dc0740`. Round 2 (after wave 4 and a ten-run measurement): merges `32779ccc6`
and `ae12800ab`, then `a3b400036` and `3dd2eeb32` (see *Round 2* below). Round 3
(the named count and the review's conditions): `760d69229`, then the merge
with round 3b, `eb814321b` (see *Round 3*). Not merged; the coordinator merges
after review.

## Outcome

In a conversation, a person says "keep an eye on my inbox folder and keep
reports/inbox-report.md current" and answers one card. They get **the item the
terminal makes** with `aforge standing add --instructions … --watch … --report …
--place …`. The following are the same:
- the instructions version (1);
- the report aforge publishes on the owner's behalf, with its sha256 receipt;
- the folder placement;
- the rules check before publication;
- the receipts and withheld codes for each run.

The `standing show` record matches a twin made at the terminal. It differs only in:
- its id;
- its project folder;
- the door (`through the chat` / `through the terminal`);
- the moments.

**The stored item is not byte-identical, and says so.** Beyond the id, the
workspace, the times and the door, a chat-made item's JSON also carries:
- `origin`: the conversation's id, transcript and turn, where a terminal item has
  none;
- `altitude: "project"`, where the terminal leaves it empty. Empty reads as
  project (`standing.go`), so both reach the same work, but the spelling differs,
  and a model that sends `altitude` can choose another reach.
- `brief.title`, the row title the model wrote;
- `adoption.proposal_id`, the card's id.

`when.words` is the model's phrasing when it sends `when_words`, and the
terminal's `when inbox/* changes` otherwise. The fields a run reads are
identical: `schema`, `specRevision`, `when` (kind, glob), `does`, `rails` and
`status`. Measured on the live items of round 1's measurement (kept homes under
`/tmp/opus-localwork/chatdoor-live-runNN`).

Nothing new runs anything. Wave 03 owns the pass, the runner, the publish
decision and the rules check, and they are unchanged. The chat door only makes
the item and binds its folder.

## The `stand` schema, before → after

```diff
+"placement":{"type":"string","description":"Work that runs (does.kind task) only: the id of an existing folder to place it in, so that folder's rules reach every run. Send it only when they named a folder. Omitted, the work is placed where this conversation is placed, or in no folder."},
 "does":{ …
-  "kind": … "task runs a brief in its own session, with a copy of its own and a cost row, the way propose_task's work runs."
+  "kind": … "task runs its instructions in a session of its own, unattended, with a cost row."
-  "brief":{"type":"string","description":"THE WORK, self-contained as propose_task's brief is: nobody will be there to ask. {{evidence}} is replaced by what the probe found."},
+  "instructions":{"type":"string","description":"THE WORK one run does, written whole: nobody will be there to ask. With does.report, say what the report holds and never to write the file or make its folder: the run can only read, and its final answer is published. {{evidence}} is replaced by what the probe found."},
+  "report":{"type":"string","description":"Only when they asked for a file kept current: its path inside the project. Each run's final answer IS the report, and aforge publishes it there, replacing the last one; the run never writes it. Never inside what when.glob watches."},
```

The description's WAKING paragraph gains one example: `"keep an eye on my inbox
folder and keep reports/inbox.md current" is file with does.kind task and
does.report`. A call that still sends `does.brief` is refused by name, not
silently dropped:
`Invalid arguments: does.brief is now does.instructions — send the work one run does as does.instructions`.
A task sent with no instructions is refused as well:
`Invalid arguments: does.instructions is required for work that runs — the work one run does, written whole`.
The stored field is unchanged: `Action.Brief`, json `brief`, is where the
terminal's `--instructions` has always gone.

## The card as rendered

This is from `TestAStandingCardDrawsTheTermsTheEngineSent`, width 100, with the
journey's exact terms:

```
╭─ ? ◦ keep an eye on my inbox ───────
│ keep an eye on my inbox folder and keep reports/inbox-report.md current
│ when · when inbox/* changes
│ where · for this project
│ does · Read the changed files in inbox/ and write a short report of new
│ decisions and requests. FOCUS: decisions
│ report · reports/inbox-report.md — aforge publishes this file; the run
│ never writes it
│ folder · Launch, where this conversation is placed — its rules reach
│ every run
│ rule · RULE-LAUNCH-7: inbox reports never quote email addresses; write
│ [redacted] instead.
│ costs · shares the day's allowance · checked every 5 minutes
╰────────
```

The options are unchanged. The engine sends each line in `StandingNotice.Terms`:
- `does ·`: the instructions, one line, clipped at 160 bytes.
- `report ·`: the path and the fixed clause.
- `folder ·`: one of three forms:
  - `<names>, where this conversation is placed — its rules reach every run`;
  - a named folder without the middle clause;
  - `none — it can be placed in one later`.
- `rule ·`: one line per rule that reaches the placement now. After five,
  `rules · and N more`. With none, `rules · none reach this work yet`.

Only work that runs gets terms. The cards for a line to say and for a rule are
unchanged.

tui3 needed one change. The card had no band for lines it was not told about, so
it could not show them. The change is a `terms` field on `standingCard` and a
loop in `standBands` that draws each term dim and wrapped, between `where ·` and
`costs ·`. Its structural test fails if the field is dropped. No tui3 suite was
run.

## Change → effect → evidence

| Change | Effect a person sees | Evidence |
| --- | --- | --- |
| `stand` schema: `does.instructions` replaces `does.brief`; `does.report`; top-level `placement` | The model spells the work the one way the terminal does. A call that still sends `does.brief` is refused and told the new name. A task with no instructions is refused and told `does.instructions`. | `TestTheStandSchemaSpellsTheWorkInstructionsOnce`, `TestAStandCallThatStillSaysBriefIsToldInstructions` |
| Default placement (`standingPlacementFor`) | Work that runs goes where its conversation is placed. A named folder wins. A conversation in no folder makes work in no folder (C12: never force a folder first). A line to say and a rule are never placed; `placement` on one is refused, and a rule names its folders with `folder_scope`. | `TestAChatCardForWorkThatRunsSaysWhatAYesAgreesTo`, `TestANamedFolderWinsAndNoFolderIsAllowed`, `TestWhatACardCouldNeverKeepIsRefusedBeforeTheCard/a_folder_for_a_line_to_say` |
| Card terms (`StandingNotice.Terms`, `standingTerms`) | Under `where ·` come the `does ·`, `report ·`, `folder ·` and `rule ·` lines above. | the same tests, and tui3 `TestAStandingCardDrawsTheTermsTheEngineSent` |
| The card's rules come from the run's own resolver | `workspace.Store.GoverningIfPlaced` is the same recursive walk as `GoverningCollections`, seeded with the folders. The standing owner's `ApplicableScope(workspace, origin session, depths)` follows, then `session.GoverningRules`: the one hold filter, which the turn's `<standing>` block and `standing show` now use too. | `TestGoverningIfPlacedIsWhatAPlacementThereGoverns`. The chat card test compares the card's rule with what the run reads from the placement the yes wrote. In the journey, the card names RULE-LAUNCH-7 and the journals for its runs carry exactly that rule. |
| Receipt `via: chat`, log `set up in the chat` | `standing show` says `set up by: person, through the chat`. | chat card test; journey |
| The folder is bound **before** the item exists, at both doors | No crash between the two writes can leave work running outside the rules the card quoted. A failed binding sets nothing up: `nothing was set up: could not be placed in Launch: …`. The terminal used to create the item first, and could leave it standing unplaced. | `TestTheFolderIsBoundBeforeTheWorkExists` (it observes Create, and the id is already placed), `TestAPlacementThatDidNotTakeLeavesNothingRunning` |
| `CheckStandingReport` at setup (card, `add`, `edit`) | A report path is refused before the card, or at the command, if its folder leads out of the project through a symlink (`the report folder resolves outside the project`) or if the path is itself a link. Publish still re-checks at the write. | `TestWhatACardCouldNeverKeepIsRefusedBeforeTheCard/a_report_folder_linked_out_of_the_project`; the journey's terminal refusal step |
| A watch with no `when_words` still says when it wakes (`standing.WatchWords`, one spelling for both doors) | The card and `show` say `when inbox/* changes`. The model's own words still win. | `TestAWatchWithNoWordsOfItsOwnSaysWhenItWakes` (from live1) |
| No timer here, or a failed install, said to the person **and to the model** | Anything that wakes on a host with no timer says `no background checks on this machine · checked only while an aforge window is open, or when you run aforge standing check`. A failed install says why, with the same `checked only while…` clause. The `stand` result now carries the same line, so the model's one-line reply cannot promise checks that do not exist. Both live transcripts did promise them (see below). The prompt no longer promises background checks unconditionally. | `TestAHostWithNoTimerSaysHowChecksHappen`, `TestALinuxTimerThatWouldNotStartSaysSoAndLeavesNothing` (the production Linux timer with only the process boundary replaced: never the success line, units removed, status not installed, and the model told the failure), `TestABackgroundInstallThatFailedSaysSo`. The journey asserts the line. |
| `standing show` reads rules with the item's conversation | What `show` lists is what the run's check reads, whichever door made the item. | the journey's `show` comparison |
| Manual and probes | New section *Keep a report current from the chat*. The terminal section says the chat makes the same order. The keeping-an-eye and home pages carry the new lines. Five new probes. | `internal/manual` suite, `make test-packed-manual` |

Old-logic proofs were recorded for two changes. In each, the change was
reverted in place, the named test failed, and the change was restored:
- tui3: dropping the `terms` field fails `TestAStandingCardDrawsTheTermsTheEngineSent`.
- `178dc0740`: with the old result, the tests fail with `the model was not told
  there is no timer` and `the model was told something other than the failed
  install`.

The other rows are pinned by tests written against the new behaviour.

## The journeys

**`TestChatDoorJourney`** (`-tags e2e`, scripted loopback model, no key, about
2s). A real `session.Agent` over the real standing store and organization DB
talks to a local model server that answers the way a model would. The steps:
1. Put rules on the folders `Launch` and `Marketing`, and place the conversation in `Launch` through `bin/aforge collections place`.
2. The terminal refuses a report linked out of the project.
3. The chat sentence produces a `stand` call. The card must carry the exact four terms above, and the no-timer line.
4. The yes creates the item.
5. A terminal twin is made in its own project with the same report path. The two items' JSON fields `schema`, `specRevision`, `when`, `does`, `rails` and `status` must be equal.
6. `bin/aforge standing check` takes the baseline. After a file changes, both runs land and publish, each with its cause and its rules check (kept after one correction).
7. A run that keeps the raw contact detail exits 4 with `2 need you`, both `held-by-rules`, and no report is published.
8. The two `bin/aforge standing show` records, normalized for id, project folder, door and moments, are identical.

**`TestRealChatDoorJourney`** (`-tags e2e`, `OPENROUTER_API_KEY`,
`deepseek/deepseek-v4-flash`, `AFORGE_DAILY_BUDGET=2`). This is the same road
with a real model choosing the `stand` call itself. It is strict: the card, the
item, the placement, the via-chat receipt, a check that lands, and a published
report with no raw contact detail.

Session regressions: the ten tests in `internal/session/standing_chatdoor_test.go`
named above. Workspace: `TestGoverningIfPlacedIsWhatAPlacementThereGoverns`.
tui3: `TestAStandingCardDrawsTheTermsTheEngineSent`.

## Live runs and spend

The brief asked for one live journey. It ran three times, one at a time, and
each run exposed a defect or proved a fix:

| Run | Head | Result | Spend | Log |
| --- | --- | --- | --- | --- |
| live1 | `b06934cf3` | **FAIL** (exit 4) | $0.0074, 10 calls | `validation/chatdoor-live1.log` |
| live2 | `4ae907108` | **PASS** | $0.0093, 11 calls | `validation/chatdoor-live2.log` |
| live3 | `178dc0740` | **FAIL** (item check, before any run) | $0.0027, 6 calls | `validation/chatdoor-live3.log` |

**live1.** The chat half held. The model called `stand` itself, and the card,
item, folder, `through the chat` receipt and no-timer line were right. Two
defects:
- The model sent no `when_words`, so neither the card nor `show` said when the
  item wakes. The fix is the `WatchWords` fallback.
- The model's instructions told the run to *write* `reports/inbox-report.md`
  and make `reports/`. The run reached for bash and write, was refused, and
  ended `waiting-on-person`.

  That refusal is the pre-existing wave-04 item 1 defect: a refused act inside
  an unattended run parks it on the person. Wave 4 closed it with grant-shaped
  belts. Round 1 told the model in the `instructions` description that "the run
  can only read"; round 2 drops that claim, which was no longer the whole truth
  once a run carries what its rules grant.

**live2.** Passed. The card read `when · when inbox/* changes`, with the
report, `folder · Launch, where this conversation is placed` and the rule
lines. The run landed. It was `checked against 1 rule(s): kept after one
correction`: the check sent back a `[Redacted]` casing. It published
`reports/inbox-report.md` (356 bytes, sha256 `675499036081…`) with no contact
detail.

The model's first `stand` call was refused (`when.kind` missing), and its
second was right. Its instructions still said "Write the result to
reports/inbox-report.md", but this run did not try to.

**Both transcripts made the same false claim about background checks.**
- live1 replied "Checks every 5 minutes, window or not".
- live2 replied "with background checks active".

The dim row under each had just said `no background checks on this machine`.
The model never saw that row. `178dc0740` puts the row in the `stand` result.

**live3.** It checks that fix on the real model. The fix held: the reply
says "Only active while an aforge window is open (this machine has no
background timer)".

The journey failed at its item check. This time the model sent no
`does.report` and put "Compile a summary report and write it to
reports/inbox-report.md" into the instructions. The card was truthful: it had
no `report ·` line, so a person reading it would see no report promised. The
item is still not the terminal's item. The test stops there, before any run,
so nothing wrote or was refused.

`deepseek/deepseek-v4-flash` chose `does.report` in 2 of 3 live sentences.
This is the open finding below. No fourth run was made.

Total live spend: **$0.0194** over 27 calls ($0.0074 + $0.0093 + $0.0027).

## Validation

**Round 2:** the same script ran at `3dd2eeb32`, with only this document
modified, and passed **13/13, STATUS 0** (`validation/chatdoor-validate-r2.log`).
Round 2 also ran the whole of `internal/session`, `internal/manual`,
`internal/standing`, `internal/workspace`, `cmd/aforge` and untagged
`internal/e2e`. Everything passed except the two upstream hand-over flakes in
*Round 2*.

**Round 1:** `validation/chatdoor-validate.sh.txt` ran at `178dc0740` on a clean tracked
tree. The result is **13/13 PASS, STATUS 0**, in `validation/chatdoor-validate.log`:

| Step | Result |
| --- | --- |
| vet, vet-e2e | PASS |
| small-suites (standing, workspace, workspaceview, manual) | PASS 8s |
| session-selected | PASS 89s |
| cmd-selected | PASS 5s |
| tui3-card (4 named tests) | PASS |
| e2e-untagged | PASS |
| laws | PASS 18s |
| changelog | PASS |
| build (`make build`) | PASS |
| packed-manual | PASS |
| localwork-journey (`TestLocalWorkJourney`) | PASS 4s |
| chatdoor-journey (`TestChatDoorJourney`) | PASS 2s |

The run before it at the same revision failed one step, `session-selected`.
Every test in it passed. The failure was the suite's checkout guard: this
document was written into the tree while the suite ran. The rerun touched
nothing.

The steps cover:
- vet, including the e2e tag;
- the standing, workspace, workspaceview and manual suites;
- `internal/session` and `cmd/aforge` narrowed by `-run` to every family this
  lane touches (standing, stand, governing, organization, card, placement,
  folder, timer, background, prefix, belt, prompt and more);
- the four named tui3 card tests;
- the untagged e2e table test;
- `make test-laws`, including the vocabulary law;
- `make changelog-check`, `make build` and `make test-packed-manual`;
- wave 03's `TestLocalWorkJourney` and this lane's `TestChatDoorJourney`.

The prompt and tool prefix is 47,942 of its 48,000-byte budget
(`TestTheFixedPrefixStaysUnderItsBudget`), before and after both merges. The
budget's belt (`v3ShapedAgent`) has no standing store, so the `stand` schema is
not in the measured prefix. What round 1 trimmed was the prompt itself: one
paragraph that repeated the schema was removed, and the background paragraph
this lane had grown was compressed.

## Round 2 — after wave 4, and the ten-run measurement

### Merges and conflict resolutions

- **`32779ccc6`** merges wave 4 (`815f5d60c`). There were two textual
  conflicts, resolved as the review prescribed:
  - **`internal/session/governing.go`.** Wave 4's slice form wins
    (`nearestPlaces` / `placementDepths`), and this lane's `nearestDepths` is
    deleted. `standingRulesIfPlaced` now folds `GoverningIfPlaced` through
    `nearestPlaces(nil, found)` and asks `ApplicableScope(…,
    placementDepths(places))`. `standing_chatdoor_test.go:247` reads the
    placement the same way.
  - **`internal/session/standing_run.go`.** `reportTarget` resolves the deepest
    existing folder with wave 4's `deepestExisting` (`readroot.go`).
    `publishStandingReport` keeps one pre-`MkdirAll` check, through
    `reportTarget`, instead of the two the textual merge would have left.
- **`ae12800ab`** merges the feature branch's newer tip (`b6c964f9e`, t03b "one
  record for direction"), which landed while this lane worked. It merged with
  no conflicts.
- **A regression of this lane's own, found by the full suite.**
  `TestTheSectionWithSchedulingStillTeachesTheMechanics` pinned "BACKGROUND
  CHECKS ARE ON AND NOBODY IS ASKED". `b06934cf3` made that sentence
  conditional, and round 1's `-run`-filtered session step never selected the
  test. It now pins the conditional mechanic. Round 2 ran the whole
  `internal/session` package.
- **Prefix:** 47,942 of 48,000 bytes before and after both merges and the seam
  fixes. The `stand` schema is not in the measured belt (see *Validation*).

### Seam fixes, each with its regression and an old-logic proof

Every regression below FAILS with its fix reverted in place, and passes with it
restored (8 of 8, recorded in the lane log).

| Fix | Seam | Effect | Regression |
| --- | --- | --- | --- |
| A file the person named is asked about as the report (measurement cause B) | Tool-result refusal. `standingNamedReport` reads the person's verbatim `words`, never the model's instructions. `does.report` is now `*string`, so an omitted report and `""` are different answers. The new `standing.Item.Watches` is the pass's own matcher, so a watched file is never mistaken for the report. | `Invalid arguments: their sentence names reports/inbox-report.md — send does.report "reports/inbox-report.md" if each run keeps that file current, or does.report "" if the work only reads it`. The model retries. The refusal was chosen over a card question because it is the smaller seam: no new option, no tui3. | `TestAFileTheSentenceNamesIsAskedAboutAsTheReport`, `TestOnlyAFileOutsideTheWatchIsAskedAbout`, `TestAnItemWatchesWhatItsPassReads` (standing) |
| Unasked spending limits (cause C) | Schema enforcement plus a card line. `standingNamedMoney` drops `per_run_usd` and `max_per_day` when there is no `cost_words`. `standingCostWords` writes the costs line from the item: any limit that differs from the quiet defaults, then the day's allowance. The stand result carries it as `costs: …`. | A limit nobody named never stands. One that was named leads the costs line: `up to $1.00 a run · at most 2 runs a day · shares the day's allowance`. | `TestLimitsThePersonDidNotNameAreDroppedAndTheCardSaysWhatBinds`. Two older pins were changed on purpose: `TestStandingPersonNamedRailsSurviveAndTheCardQuotesThem` now pins the item-written line, and the negative-limit test names its limit. |
| `report · none` | Card line | Work that runs and keeps no file says `report · none — no file is kept current`. | `TestAFileTheSentenceNamesIsAskedAboutAsTheReport` (second half) |
| "the run can only read" | Schema description | Removed; the reason that stays true is "its final answer is published". | description text (budget test unaffected) |
| Inherited placement under collections' law (review) | `Agent.mayBindFolders`, shared with `collections place` | A delegated principal can bind no folder, named or inherited: `placing work in a folder needs the person's answer in a conversation — this conversation is placed in Launch`. A conversation in no folder is not refused. | `TestAStewardCannotBindTheConversationsFolderToWork` |
| Two folders (review) | Card and log wording | `folder · Launch, Marketing, where this conversation is placed — their rules reach every run`; the log reads `placed in folders …; their rules reach this work`. | `TestWorkInTwoFoldersSaysBothAndIsPlacedInBoth` |
| Hold-limit gate on the card (review) | Card line | More than `governingHoldLimit` (64) rules reaching the placement reads `rules · 65 reach this work, more than the 64 a run can carry — every run would stop until they are narrowed`. The conversation's own gate stops earlier when the conversation itself sits over the limit. | `TestACardSaysWhenItsRulesAreMoreThanARunCanCarry` |
| Background line after the notice (review) | Result feedback | The model's result always carries what checks the item: `checks every 5 minutes, window or not · …` or `background checks are not running · …`. The person's row stays once-ever, the pinned design of `TestTheBackgroundLineIsSaidOnceEver`. | `TestTheModelHearsWhatChecksTheWorkAfterTheOneLine` |

The manual pages (standing-orders, keeping-an-eye, home) carry the new lines,
and three probes were added. The home card example's invented
`about $0.02 a run` became the line the engine now writes.

### Two live measurements, ten runs each

The same driver ran both (`/tmp/opus-localwork/zz_chatdoor_measure_e2e_test.go`,
untracked, removed after each), serially, on `deepseek/deepseek-v4-flash`.
Round 1's analysis is in `validation/chatdoor-live10.md`, which is left
untracked in the lane's worktree as that brief asked; its numbers are in the
table below. Round 2's logs are `validation/chatdoor-live2-run01..10.log`.

| Measure | Round 1 at `2495b6526` | Round 2 at `3dd2eeb32` |
| --- | --- | --- |
| The journey's own criteria (`TestRealChatDoorJourney`'s assertions) | **6/10** | **7/10** |
| The twin identical, strict | 2/10 (1 raw + 1 driver artifact) | **2/10** (07, 09) |
| The twin identical, driver's `acceptance` artifact removed | 2/10 | **4/10** (03, 07, 09, 10) |
| Runs refused a command (`nobody to ask`) | 3 runs, 11 refusals | **0** |
| Calls to an absent tool | — | 4 (06, 08, 10), none stopped a run |
| `does.report` on the item | 9/10 | **10/10**. The new refusal fired in 01 and 04, and both retries were right. |
| Unasked limits standing | 7/10 | **0/10**. 5 calls sent them, and all were dropped. |
| `costs ·` empty on the card | 8/10 | **0/10** |
| Spend | $0.0497, 103 calls | **$0.1085, 115 calls** |

Round 2 costs about twice as much per run. Wave 4's per-rule check makes more
judgement calls, and several runs landed on slower, dearer endpoints; the
ledger does not separate the two.

**Round 2's three failures stay failures:**
- **Run 05. Model (chat), then a harness law working as designed.** Before
  proposing anything, the conversation itself called `write` and created
  `reports/inbox-report.md` as a placeholder. The first run's report was then
  held: `reports/inbox-report.md is not what aforge last published there — it
  was changed, or it was there before aforge wrote it` (`report-changed`,
  `needs-you`). Wave 3's never-write-over-the-person law was right; the card
  gave no warning. **Proposed seam:** preview that gate on the card and in the
  result when the report path already holds a file aforge never published, the
  same way the hold-limit gate is now previewed:
  `report · reports/inbox-report.md — a file aforge did not write is there; the
  first report will wait for you`.
- **Run 06. Test.** A healthy run (one absent-tool call, then reads) on an
  endpoint answering in 25–40 s a call was cut off when the driver's 3-minute cap on one
  `aforge standing check` (`journey.run`) killed it. **Fix:** the driver should
  give the pass the item's own deadline. This is test-side only.
- **Run 08. Model (run).** It called absent `stat` and `bash`, carried on, and
  ended its final answer with `</report>` and no opening line:
  `unopened-report`, `failed`. **Proposed seam:** result feedback. Send a
  malformed report envelope back to the run once (`your report has a closing
  line but no opening line — reply again with the whole report between the
  lines`), on the same one-correction road the rules check already uses.

**Still keeping the twin from identity:**
- `when.words` in the model's phrasing: 01 and 08. This is measurement cause D;
  D1 was not in this round.
- `max_steps: 20` sent unasked, which `standing add` cannot express: 02, 04 and
  06. This is cause E; E1 was not in this round.
- `acceptance`: 03, 06 and 10. This is the driver artifact; `standing add
  --acceptance` exists, and the driver did not pass it.

**A truthfulness slip that still happens.** Run 01's reply after the yes said
"anything over $0.50 in a single run won't proceed". The $0.50 limit it had
sent was dropped, and its result said `costs: shares the day's allowance`.
**Proposed seam:** result feedback that names what was dropped (`the limits
you sent were not kept — the person named none`), so the model has a fact to
say instead of its own number.

### Upstream flakes, reported and not fixed

`TestHandingTheSameDecisionOverTwiceSaysItIsAlreadyHandedOver` and
`TestHandingAYourCallToTheModelAndItsResolveAreOneRoad` (`tools_tasks_test.go`)
each failed once in a full `internal/session` run. They reproduce under
`-count=400 -cpu 1,2,8` on the untouched feature-branch head `b6c964f9e`, in a
throwaway detached worktree since removed, with 29 and 8 failures in 1,200 runs.
So they are a race in task hand-over, not this lane's, and a bug report for its
owner.

## Round 3 — a named count of runs, the review's conditions, and round 3b

### The blocker: a count the person named was dropped

Round 2's `standingNamedMoney` kept `rails.per_run_usd` and `rails.max_per_day`
only when the call sent `cost_words`. The schema described `cost_words` as money
only: "When the person named money, quote their limit in their words". So "no
more than 3 runs a day", sent faithfully as `max_per_day: 3` with no
`cost_words`, became the default 10. The card said only `shares the day's
allowance` and never mentioned the change.

The fix is in `tools_standing.go`:
- **`cost_words` covers a count as well as money.** "When the person named a
  limit — money, or how many runs — quote it in their words: "at most a dollar
  a run", "no more than 3 a day". A rail sent without it is dropped."
- **Each rail has its own fate.** `standingNamedLimits` marks each one unsent,
  kept or dropped, where round 2 had one money gate for both, and it replaces
  `standingNamedMoney`.
- **Every rail the call sent is on the costs line**, even when it equals the
  default. A dropped rail is shown with `(the default)`: `at most 10 runs a day
  (the default) · shares the day's allowance`.
- **The tool result names every dropped limit.** `limits not kept, because
  cost_words quoted no limit the person named: rails.max_per_day 3 — say only
  what costs: says`. This is fix 3 from the round-2 measurement (*The proposed
  seam* above).

**Regression:** `TestANamedCountOfRunsStandsOnItsOwnWordsAndADroppedOneIsNamed`
(`standing_chatdoor_test.go`). It sends `max_per_day` with and without
`cost_words`. Its pre-fix failures were recorded at `1c587d446` in a throwaway
detached worktree, since removed:

```
standing_chatdoor_test.go:716: cost_words still speaks only of money, so a named count has no words to stand on: "When the person named money, quote their limit in their words — \"at most a dollar a run\". Omit when they named none; aforge quotes the shared allowance."
```

With that assertion skipped, it failed on the card:

```
standing_chatdoor_test.go:740: the card hides the default that replaced the count: "shares the day's allowance", want "at most 10 runs a day (the default) · shares the day's allowance"
```

### The review's other conditions

Each regression below FAILS with its fix reverted in place and passes with the
fix restored. Each fix was reverted in this tree and then restored byte for
byte from a copy.

| Fix | Effect | Regression | With the fix reverted |
| --- | --- | --- | --- |
| **The named-report refusal.** Two files, `~` and absolute paths, `..`, and files the watch reaches. `standingNamedReport` filters every candidate through `standingCouldReport`, which is the item's own `Validate` with that report (so no absolute path and no `..`) and no `~`, and through `Watches`. It removes duplicates and lists every file that is left. `standingLooksLikeFile` needs an extension of 2–8 letters or digits that starts with a letter, so `e.g.` and `v1.2` are not files. | One file keeps round 2's wording. Several read `their sentence names reports/a.md, notes/b.md — send does.report with the one each run keeps current, or does.report "" if the work only reads them`. | `TestOnlyAFileOutsideTheWatchIsAskedAbout`, rewritten on a real item from `standingItem`. Round 2's hand-built item failed `Validate`, so that test could not see the difference. | `:554: a file that could never be the report was asked about: "Invalid arguments: their sentence names inbox/sub/notes.md, ~/notes.md, /etc/ho…`, and for two files only the first was named |
| **A negative rail with no `cost_words` is refused, not dropped.** | `a per-run budget cannot be negative` / `an item needs a max per day` | `TestANegativeLimitWithNoWordsIsRefusedNotDropped` | `:804: a card was drawn for a negative limit` |
| **`folder_scope` goes through `mayBindFolders`.** Round 2 checked only for a steward. | `folder rules need the person's answer in a conversation`, for a steward and when nobody is there to ask | `TestAFolderRuleIsBoundOnlyWhereFoldersMayBeBound` | `:828: nobody to ask: a folder rule answered "nobody is here to say yes — this can only be set up in a conversation…"`, a later gate's refusal |
| **A named limit shows even at the default, and there is never a `$0.00`.** | `at most 10 runs a day · …`; `per_run_usd: 0` reads `no per-run limit` | `TestANamedLimitShowsEvenAtTheDefaultAndNoCapIsNeverZeroDollars` | `:782: … said "at most ten a day" reads "shares the day's allowance"`, and `… said "no limit per run" reads "up to $0.00 a run · …"` |
| **The 64 KiB gate is on the card** beside the 64-rule gate (`governingPromptBytes`). | `rules · their words come to N KiB, more than the 64 KiB a run can carry — every run would stop until they are narrowed` | `TestACardSaysWhenItsRulesAreMoreWordsThanARunCanCarry` | `:848: the card's terms end "rules · and 15 more" (9 terms)` |

Two corrections are to text:
- **The attribution in *Open finding*.** Round 2 said the refusal was what the
  review directed. The review offered a refusal or a card question, and this
  lane chose the refusal as the smaller seam.
- **The manual's "same order as `standing add`" claim.** It now narrows the
  claim to "the same order as far as a run can tell" and names the
  differences. The origin reads `set up in the chat` and `through the chat`.
  The record keeps the conversation it came from. The reach is written
  `project` where the terminal leaves it empty, which reads as project. The
  wake may be worded the chat's way.

The manual carries `at most 3 runs a day`, `(the default)`, `no per-run limit`
and the 64 KiB gate, and two probes were added.

### Merge with round 3b and its two conflicts

`eb814321b` merges `origin/codex/personal-ai-backend` at `e373ab411` (round 3b).
Both conflicts keep round 3b's behaviour:
- **`internal/session/standing_run.go`, the fence and publish seam.** Round 3b's
  `reportFile`/`reportFileAt` (the first report created by a hard link, and the
  fence at the act) replaces `unchangedSince`. `publishStandingReport` is round
  3b's body with one change: this lane's `reportTarget(workspace, report)`
  supplies the root and the target, in place of the inline
  EvalSymlinks/join/`deepestExisting` check. The semantics are the same.
  `CheckStandingReport` stays for the card.
- **The answers path, in the change entry.** Round 3b's reworded inbox/answers
  line is kept verbatim, and this lane's lines follow it. This lane never
  touched `answers.go` or `question.go`. A standing answer drained from the
  doorstep still goes through `ResolveQuestion` to `ResolveStanding`. An answer
  nobody is waiting on is a no-op, so a replay cannot run placement or creation
  a second time.

### Known gap, recorded for the orchestrator (not fixed)

**`TestTheFixedPrefixStaysUnderItsBudget` never weighs `stand`, `collections` or
`shared_context`.** Its belt, `v3ShapedAgent`, has no standing store and no
`Organization`, so none of the three tools is on it.
`TestTheFixedPrefixOfEveryShapeIsMeasured` weighs `stand` through
`conversationDoor`, but it is a measurement with no budget, and it has no
`Organization` either.

They were weighed on the same belt with both seams wired, by a throwaway test in
a detached worktree (since removed):

| Revision | Measured by the test | With the three tools | `stand` | `collections` | `shared_context` |
| --- | --- | --- | --- | --- | --- |
| `e373ab411` (round 3b, before this merge) | 47,911 | 61,327 | 10,165 | 1,456 | 1,792 |
| `eb814321b` (this lane merged) | 47,942 | 62,201 | 11,008 | 1,456 | 1,792 |

So a conversation with both seams wired, which is the interactive door's shape,
sends about 62 KB against a 48,000-byte budget. Most of that gap is on the
feature branch already. This lane's share is 843 bytes of `stand` schema and 31
bytes of prompt.

The fix belongs to the budget's owner. One route is to wire a standing store and
an `Organization` into `v3ShapedAgent` and then decide the budget or the trim.
Either change moves a ratchet, so it is out of this lane.

### Validation

`chatdoor-validate-r3.sh.txt` ran twice on `eb814321b` against a tracked-clean
tree, with `GOMAXPROCS=4 GOFLAGS=-p=2`. Both runs passed 14 of 14 steps. The
second log is `chatdoor-validate-r3-eb814321b.log`.

The steps:
- vet and the e2e vet;
- the `standing`, `workspace` and `workspaceview` suites;
- **the whole `internal/manual/...`** suite;
- **the whole `internal/session`** suite, unfiltered (226 s);
- the selected `cmd/aforge` tests and the four tui3 card tests;
- the untagged e2e table;
- `make test-laws`, `make changelog-check`, `make build` and `make
  test-packed-manual`;
- `TestLocalWorkJourney` and `TestChatDoorJourney`.

The two task hand-over race tests under *Upstream flakes* did not fail in
either run. They remain the known race, and a separate lane is fixing them.

The round-1 ten-run measurement proposal is now tracked beside this file as
`validation/chatdoor-live10.md`. It is the receipt for the causes and fixes that
round 2 and this round answer.

## Decisions taken inside the brief

- **Default placement.** Work goes in every folder the conversation is placed in
  directly (depth 0), at most 32. A conversation in no folder makes work in no
  folder, and the card says it can be placed later. Inherited ancestors are not
  copied as placements, because they reach the work through the walk.
- **A placement needs a person, whether named or inherited.** Round 1 let a
  steward inherit the conversation's folders. The review ruled that every
  binding goes through collections' law, so round 2 refuses both under a
  delegated principal. The refusal is `placing work in a folder needs the
  person's answer in a conversation`, with `— this conversation is placed in
  <names>` when the folder was inherited.
- **The no-timer line is said every time** something that wakes is set up, not
  once ever. The once-ever marker announces a switch thrown on the machine.
  This line is a fact about each item agreed to.
- **Place before create, at both doors.** The item id is minted first, then the
  folder is bound, then the item is created. If creation fails, the placement is
  undone. This changed the terminal's `standing add`, deliberately.
- **Only work that runs gets terms.** A line to say keeps no rules and writes no
  report.

Nothing is paused. Both the placement default and the card wording stay within
what the brief stated.

## Open finding for review (round 1; answered in round 2)

**The model does not always choose `does.report`.** On
`deepseek/deepseek-v4-flash` it chose the field in 2 of 3 live runs, and in 9 of
10 in the measurement. Round 1 left this open. It said that reading the
model's *instructions* for a path would be a band-aid.

Round 2 answers it at the tool-result seam. The review offered a refusal or a
card question; this lane chose the refusal as the smaller seam. It reads
the *person's own sentence* (`words`, verbatim), not the model's text. A file
named there, when the call left `does.report` out, is refused before any card
with both honest answers:
- the path, if each run keeps that file current;
- `""`, if the work only reads it.

A file the watch itself reaches is never asked about (`standing.Item.Watches`).
Wave 4's grant-shaped belts answered the other half: a run cannot reach for a
tool it was not granted.

## Boundaries (stated, not hidden)

- **Wave-04 item 1 is fixed by wave 4, not here.** An unattended run now
  carries only the tools its rules grant, so under the default rules there is
  no `bash` or `write` to reach for. Round 2's measurement checks this live.
- **An empty `costs ·` when the model sent rails without `cost_words`** was
  true in round 1. Round 2 fixed it: unnamed limits are dropped and the costs
  line is written from the item. Round 3 keeps a count of runs the person named,
  and it shows and names every limit it drops (*Round 3*).
- **The chat is driven through the session's seams, not the TUI.** Both
  journeys run a real `session.Agent` in the test process, with the card
  answered through `ResolveStanding`. The card as drawn is proved by the tui3
  structural test, not by a tmux screen.
- **The live assertion does not read the model's prose.** Whether a reply
  promises checks is judged from the logged transcript, not asserted. Such a
  check would be a word list over model output.
