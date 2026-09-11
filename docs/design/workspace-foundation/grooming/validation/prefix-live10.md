# Prefix budget lane — ten live chat-door journeys per text

`TestRealChatDoorJourney` was run ten times serially per text on
`deepseek/deepseek-v4-flash`, with the driver detached (`setsid nohup`,
exit marker). The spend was capped at $0.50 over all four batches; it came to
$0.2978.
The acceptance bar was the round-2 baseline in `BUILD-CHATDOOR.md`:
7/10 journeys, 0 refusals, `does.report` 10/10.

Columns:
- **Unasked limit: sent / stood.** The person's sentence names no limit.
  "Sent" means a `stand` call carried `rails` or `cost_words`. "Stood"
  means a limit other than a `(the default)` one is on the card.
- **Card named the reach.** The card carries
  `folder · Launch, where this conversation is placed`, and the item's
  altitude is shown after it.
- **Chat called write.** The conversation itself wrote a file before or
  while proposing.
- **`op` omitted.** The count of `op is required` refusals.

## A — the trimmed text at `dbed84b09` (recognition on the page)

| Run | Journey | Why (if failed) | does.report filled | Unasked limit: sent / stood on the card | Card named the reach (folder line; altitude) | Chat called write | `op` omitted | Unasked model |
|---|---|---|---|---|---|---|---|---|
| 01 | FAIL | report-changed | yes | no / no | yes; project | 1 | 1 | no |
| 02 | FAIL | report-changed | yes | no / no | yes; project | 1 | 1 | no |
| 03 | PASS | — | yes | yes / no | yes; project | 0 | 0 | no |
| 04 | FAIL | report-changed | yes | no / no | yes; project | 1 | 0 | no |
| 05 | FAIL | report-changed | yes | no / no | yes; project | 1 | 0 | no |
| 06 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 07 | FAIL | cut-off | yes | no / no | yes; project | 1 | 0 | yes |
| 08 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 09 | FAIL | report-changed | yes | yes / no | yes; project | 1 | 0 | no |
| 10 | FAIL | report-changed | yes | no / no | yes; project | 1 | 0 | no |

**3/10** journey · does.report 10/10 · unasked limit sent 2/10, stood 0/10 · card named reach 10/10 · chat wrote the report 7/10 · `op` omitted 2/10 · unasked model 1/10

Failure modes:
- **`report-changed`, 6 runs.** The conversation wrote
  `reports/inbox-report.md` itself before proposing ("Let me create the
  initial report and set up the watch"). The pass then held the first report
  under the never-write-over-the-person law.
- **Run 07.** It sent `does.model` `anthropic/claude-sonnet-4-20250514`
  unasked, and the run was cut off (`not a valid model ID`).

**Below the bar.**

## B — A with five field sentences restored verbatim (`op`, `instructions`, `report`, `model`, page opener)

| Run | Journey | Why (if failed) | does.report filled | Unasked limit: sent / stood on the card | Card named the reach (folder line; altitude) | Chat called write | `op` omitted | Unasked model |
|---|---|---|---|---|---|---|---|---|
| 01 | FAIL | report-changed | yes | yes / no | yes; project | 1 | 0 | no |
| 02 | FAIL | the card did not say what governs the work: | yes | no / no | no; project | 1 | 0 | no |
| 03 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 04 | PASS | — | yes | yes / no | yes; project | 0 | 0 | no |
| 05 | FAIL | report-changed | yes | no / no | yes; project | 1 | 0 | no |
| 06 | PASS | — | yes | yes / no | yes; project | 0 | 0 | no |
| 07 | FAIL | report-changed | yes | yes / no | yes; project | 1 | 0 | no |
| 08 | FAIL | report-changed | yes | no / no | yes; project | 1 | 0 | no |
| 09 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 10 | FAIL | the card did not say what governs the work: | yes | no / no | no; project | 1 | 0 | no |

**4/10** journey · does.report 10/10 · unasked limit sent 4/10, stood 0/10 · card named reach 8/10 · chat wrote the report 6/10 · `op` omitted 0/10 · unasked model 0/10

`op` omissions dropped from 2 to 0. Pre-writes stayed high, at 6/10. Two runs
sent `placement` unasked, and their card read `folder · Launch — its rules
reach every run`. **Below the bar**, so the field trims were not the main
cause.

## C — control: the old text at `05c8aec12`, exported with `git archive` and built

| Run | Journey | Why (if failed) | does.report filled | Unasked limit: sent / stood on the card | Card named the reach (folder line; altitude) | Chat called write | `op` omitted | Unasked model |
|---|---|---|---|---|---|---|---|---|
| 01 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 02 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 03 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 04 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 05 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 06 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 07 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 08 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 09 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 10 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |

**10/10** journey · does.report 10/10 · unasked limit sent 0/10, stood 0/10 · card named reach 10/10 · chat wrote the report 0/10 · `op` omitted 0/10 · unasked model 0/10

The old text never pre-wrote the report. **This is the reference for the
autopsy.**

## D — final: the old tool description and page section restored, the schema trims kept (plus `placement` restored)

| Run | Journey | Why (if failed) | does.report filled | Unasked limit: sent / stood on the card | Card named the reach (folder line; altitude) | Chat called write | `op` omitted | Unasked model |
|---|---|---|---|---|---|---|---|---|
| 01 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 02 | PASS | — | yes | yes / no | yes; project | 0 | 0 | no |
| 03 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 04 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 05 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 06 | PASS | — | yes | no / no | yes; project | 0 | 0 | no |
| 07 | FAIL | report-changed | yes | no / no | yes; project | 1 | 0 | no |
| 08 | PASS | — | yes | yes / no | yes; project | 0 | 0 | no |
| 09 | FAIL | report-changed | yes | no / no | yes; project | 1 | 0 | no |
| 10 | FAIL | the card did not say what governs the work: | yes | no / no | no; project | 0 | 0 | no |

**7/10** journey · does.report 10/10 · unasked limit sent 2/10, stood 0/10 · card named reach 9/10 · chat wrote the report 2/10 · `op` omitted 0/10 · unasked model 0/10

Failures:
- **Runs 07 and 09.** `report-changed` from a pre-write.
- **Run 10.** `placement` was sent unasked, so the card wording differs.

**Meets the bar at 7/10.** It is still below today's control (10/10): the
schema trims that remain may cost about 2 pre-writes in 10.

## Autopsy

The cause was the move of recognition out of the tool description.
- The old description carries the near-exact recipe for this sentence ("keep
  an eye on my inbox folder and keep reports/inbox.md current" is file with
  does.kind task and does.report).
- It also says "doing a standing sentence once instead of proposing it
  answers a request they did not make" at the moment the model chooses between
  proposing and doing.

With those sentences on the page only, the conversation began doing the work
(writing the report) in 7 of 10 runs, against 0 of 10 for the control.
Restoring them brought the rate back to 2 of 10 and the journey to 7 of 10.
