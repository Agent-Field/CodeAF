# Chat door, ten live runs: pass rate, autopsy, seam-level fixes

2026-09-11. Measurement only; no tracked source changed. Revision `2495b6526`,
with `bin/aforge` built clean (`dirty=false`, sha256 `241cf472…`). Model
`deepseek/deepseek-v4-flash`. Ten runs, one at a time, 03:56–04:16Z.

## Bottom line

| Measure | Result |
| --- | --- |
| The committed `TestRealChatDoorJourney` criteria | **6/10** (03, 05, 06, 07, 09, 10) |
| All of that plus the terminal twin identical | **2/10** (05; 09 once a driver artifact is removed). The driver's raw result was 1/10. |
| A card drawn, an item made, placed, `through the chat`, and in `show` | 10/10 |
| An item of the right shape (file watch, task, `does.report`) | 9/10 |
| A report published with no raw contact detail | 6/10, and the rules check kept all 6 |
| A false claim of background checks in the reply after the yes | 0/10. Eight said checks happen only while a window is open; two did not mention checks. This validates `178dc0740`. |
| **Spend** | **$0.0497 over 103 calls** (each home's usage ledger) |

The four failures have two causes:
- **A refused command parked the run** (01, 02, 08).
- **The model left out `does.report`** (04).

Three further causes make the item differ from the terminal's even when the run
passes:
- **Money limits the model chose, hidden from the card** (8/10 sent limits).
- **Wake words in the model's own phrasing** (5/10).
- **A field the terminal cannot express** (1/10).

## Method

The driver is `/tmp/opus-localwork/zz_chatdoor_measure_e2e_test.go`. It was
untracked, copied into `internal/e2e` only for the runs, then removed. It runs
`TestRealChatDoorJourney`'s road with every stage recorded instead of stopping
at the first failure, plus the terminal twin from the scripted journey:
1. chat → card → yes;
2. item shape and card terms;
3. a terminal twin: `standing add` with the chat item's own words, instructions,
   glob, report and folder, in a project of its own;
4. before either item runs, JSON fields `schema specRevision when does rails
   status` compared, and `standing show` compared through `sameShape`;
5. baseline check → one inbox change → check → the run's outcome, publication,
   receipt, raw contact detail → the chat's `show`.

Each run writes one `MEASURE {json}` line. Logs are
`/tmp/opus-localwork/chatdoor-live-run01..10.log` and the homes are kept at
`/tmp/opus-localwork/chatdoor-live-runNN/`. `/tmp/opus-localwork/chatdoor-autopsy.py`
reads the chat transcript, the stand calls and their results, and every
unattended run's tool calls and refusals.

## The ten runs

| Run | Item | Card | Twin vs terminal | Run | Published | Spend | Verdict |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 01 | ok | ok | wake words | **needs-you**: bash ×4 refused | — | $0.0066 | FAIL: cause A |
| 02 | ok | ok | wake words, limits $0.50, acceptance† | **cut at 3 min** after bash ×2 refused | — | $0.0043 | FAIL: cause A |
| 03 | ok | ok | limits $1, acceptance† | landed, check kept | yes | $0.0063 | pass (committed criteria) |
| 04 | **no `does.report`** | no report line | wake words, limits $2, `maxSteps 30` | landed | **nothing to publish** | $0.0020 | FAIL: cause B |
| 05 | ok | ok | identical | landed, check kept | yes | $0.0057 | **PASS strict** |
| 06 | ok | ok | wake words, limits 24/day | landed, check kept | yes | $0.0060 | pass (committed criteria) |
| 07 | ok | ok | wake words, limits $1 | landed, check kept | yes | $0.0060 | pass (committed criteria) |
| 08 | ok | ok | limits $0.50 + 24/day | **cut at 3 min** after bash ×5 refused | — | $0.0072 | FAIL: cause A |
| 09 | ok | ok | identical (show differed only by a driver artifact†) | landed, check kept | yes | $0.0029 | **PASS strict** (after the artifact) |
| 10 | ok | ok | limits $2, acceptance† | landed, check kept | yes | $0.0027 | pass (committed criteria) |

† These are driver artifacts, not product differences (see below).

## Autopsy of the four failures

**Run 01. Cause A: model instruction, then a harness policy.** The model wrote
into the instructions: "For each new or updated file, extract: filename, size, …
and last modified time". The run's evidence already carried both facts:
`ALL MATCHING FILES: inbox/today.md  148 bytes  2026-09-10T23:56:37-04:00`.

The run read the file, then called `bash` four times (`stat -c '%s %Y'`,
`stat -c '%s'`, `wc -c`, `python3 -c "os.stat…"`). Each came back
`refused in a task: default — nobody to ask`. The run then finished a closed
`<report>`, but `firingEnd.needs` was set (`standing_publish.go:273`). Outcome:
`needs-you`, `withheld: waiting-on-person`, exit 4, nothing published.

**Run 02. Cause A, cut short by the driver.** The instructions asked for
"filename, size, and last modified time". The run was refused on `bash ls -la`
and `bash stat --format=…`, then went on calling `ls`. The endpoint
(DigitalOcean) took about 25–30 s a call, so the driver's 3-minute cap on one
`aforge standing check` (`journey.run`) killed the check. The run was left
`admitted`. Both refusals had already landed, so the run could only have ended
`needs-you`.

**Run 04. Cause B: model field omitted.** The first `stand` call sent `does` as a
JSON string and was refused (`does takes an object`). The second call was valid
but had no `does.report`. Its instructions said the report goes "between a
<report> and a </report> line … so it can be published to
reports/inbox-report.md".

The card was truthful: it had no `report ·` line. The item is a plain task. The
run landed, and nothing was published, because the item owes no report. The
person's file is never kept. The model also sent `max_steps: 30` and a
$2-per-run limit, neither asked for.

**Run 08. Cause A, cut short by the driver.** The instructions were "a listing
of all files in inbox/, with their sizes and last modified times", plus absolute
`/tmp/...` paths. The run was refused five times, on `ls -la`, `stat`, a
`find -exec stat` pipeline, `ls -la inbox/` and `pwd; ls -la`. The driver's
3-minute cap cut the check. It could only have ended `needs-you`.

## Causes grouped, with seam-level fixes

Every fix below sits at a seam: a schema constraint, a card line or refusal, a
belt, or the result a model reads. None is prompt tuning.

### A. A refused command parks the run: runs 01, 02 and 08 (3/10)

*Attribution:* the model asks for file metadata and the run reaches for `bash`;
the harness then withholds a finished report because of the refusal. Five of
ten instruction sets asked for sizes or times (01, 02, 08, 09, 10). Three of
those five runs reached for bash, and all three failed.

The facts were already in the run's evidence (`WHAT THE CHECK FOUND`), so this
is not a missing capability. The run has a verb that will always be refused,
and the model uses it.

- **A1, belt seam, recommended.** Apply "absent, not broken" to unattended
  work. When the standing run's approval policy has no rule that could ever
  allow a `bash` call, leave `bash` (and `write`/`edit` by the same test) off
  that run's belt. This is how `memoryTools` is written, and the law says it
  plainly: a capability that cannot work is absent. A person who banks a bash
  rule gets bash back.

  It does not touch "UNATTENDED MEANS WHAT WAS ALREADY ALLOWED"
  (`internal/standing/standing.go`). It only stops advertising what that law
  will refuse.

  *Where:* the run's session is `standingRunConfig` (`standing_run.go:1258`,
  `AskConsent=false`, `InTask=true`). Its belt is `belt()` over that config, and
  the omission would read the same approval policy `consent.go:282` refuses
  with. (`standing_run.go:383` is the probe's belt, not the run's.)

  *Regression:* a standing run with no banked bash rule has no `bash` on its
  belt; with an `allow bash stat*` rule it has one.
- **A2, result-feedback seam.** A refusal inside an unattended run returns
  what the run can still use, generated from its belt, not written as prose:
  `refused in a task: default — nobody to ask · this run can use: read, ls,
  find, grep; the watched files' sizes and times are in WHAT THE CHECK FOUND`.
  This helps only if the refusal stops parking the run, so it goes with A3,
  or it is the fallback where A1 cannot remove the verb (a banked rule that
  allows some commands and not others).
- **A3, publish-decision seam: the owner decides. This is wave-04 item 1.** A
  refusal the run moved past, followed by a finished and closed report,
  publishes the report and records the refusal on the run instead of
  withholding. That changes a stated law ("anything that would have asked
  stops the run"), so it is not a measurement lane's call. A1 removes the
  commonest trigger without it.

### B. The model leaves out `does.report`: run 04 here, live3 earlier (1/10 here)

*Attribution:* model; the field is optional and was skipped. The sentence names
the file, and the instructions even say "published to reports/inbox-report.md".

- **B1, schema constraint, recommended.** Make a file kept current a kind, not
  an optional field. `does.kind` gains `report`, with `report` and
  `instructions` both required, and the engine refuses `report` without a path.
  It is stored as the same typed item (`Action{Kind: task, Report: path}`), so
  there is still one spelling in the store and at the terminal.

  An enum is a choice the model has to make; an optional field is one it can
  skip. This removes the free-text reading of "published to …" as the only
  signal.
- **B2, card seam.** Work that runs with no report says so:
  `report · none — each run's answer stays in its run folder; no file is kept
  current`. This is the same shape as the existing `folder · none — it can be
  placed in one later`. The person can then answer "change" before the yes.
  Today the absence is silent.

### C. Money limits the model chose, hidden from the card: 8/10 sent limits, 7/10 differ from the terminal's

*Attribution:* the model ignores "omit rails and cost_words unless they named a
limit". The harness then draws `costs ·` from `cost_words` verbatim
(`standingCostWords`), so with no `cost_words` the costs line is **empty**.

The person answered yes to $0.50, $1 or $2 a run, or 24 runs a day, without
seeing it. The replies after the yes then stated the limit ("Costs up to
$0.50/run, max 10 runs/day"): it was disclosed after agreement. A $0.50 limit
can cut a real run short.

- **C1, card seam, recommended.** The engine draws the costs line from the
  typed limits: `up to $0.50 a run · at most 24 runs a day`, the same way
  `IntervalWords` keeps the cadence out of free text. `cost_words` is only
  appended as the person's own quote. One source of truth, and a limit can
  never be on the item without being on the card.
- **C2, schema constraint.** Limits sent without `cost_words` are refused:
  `Invalid arguments: rails are a limit the person named — send cost_words
  quoting it, or omit rails`. This makes an unasked limit a deliberate act
  rather than a default the model fills in.

### D. Wake words differ from the terminal: 5/10 (01, 02, 04, 06, 07)

*Attribution:* the harness, by this lane's own choice ("the model's words
win"). `show` then reads `wakes whenever a file appears or changes in inbox/`
where the terminal reads `wakes when inbox/* changes`.

- **D1, schema constraint, recommended.** For a trigger the types fully
  determine (`when.kind file`: the glob is the whole schedule), the words are
  the engine's (`standing.WatchWords(glob)`), and `when_words` is not read, as
  it is already not read for `hold`. The model's reading of a cadence still
  matters for `at`/`every`, where words carry what the person said. This
  follows one spelling per concept, and both doors' `show` read the same.

### E. A field the terminal cannot express: 1/10 (04, `max_steps: 30`)

*Attribution:* the harness; the `stand` schema is wider than `standing add`,
which has `--acceptance`, `--per-run-usd`, `--max-per-day` and `--model` but no
`--max-steps`. So the chat can make an item the terminal cannot.

- **E1, one vocabulary across doors.** Either `standing add --max-steps` exists
  or `max_steps` leaves the `stand` schema. The door vocabulary law should
  check the two sets against each other.
- **E2, card seam, same family.** `acceptance` was sent unasked in 3/10 (02,
  03, 10) and is not on the card. The card's terms should gain
  `done when · <acceptance>` whenever it is sent, because it governs how the
  run is judged.

## Observed, not failures

- **Refused `stand` calls always recovered.** Six of ten runs had one or two
  refused `stand` calls: `op` missing ×3, `when.kind` missing ×2, `does` sent as
  a JSON string ×2. Every one was corrected on the next call. The
  refusal-feedback seam works; each cost about one extra call. No fix proposed.
- **Rules.** The rules check ran on all six published reports and kept them.
  No raw `priya@example.com` or `555 0100` was published.
- **Absolute paths in instructions (08, 09).** The model wrote `/tmp/.../project`
  into the free text, which ties the item to one checkout. This is noted, not
  fixed: the instructions are free text, and rewriting them would be a
  band-aid.
- **The false background claim is gone.** Replies after the yes are in each
  home's chat transcript. None claims background checks. Runs 02, 03, 04, 05,
  07, 08, 09 and 10 say checks happen only while a window is open. Runs 01 and
  06 do not mention checks.

## Driver artifacts (attributed to the test, not the product)

- **`acceptance` not mirrored into the twin (02, 03, 10).** `standing add
  --acceptance` exists, and the driver did not pass it, so `does` differed by
  that field alone. Run 03's twin JSON would otherwise have differed only in
  its limits.
- **`sameShape` does not normalize the chat project's path inside the
  instructions (09).** The twin's copy keeps the chat's path while the chat's
  own is tokenized, so `show` differed on that token alone.
- **`journey.run` caps one command at 3 minutes (02, 08).** It killed
  `standing check` on a slow endpoint. Both runs had already been refused, so
  the verdict stands, but a live driver should give the pass the item's own
  deadline.

## Suggested order

1. **A1, C1 and B1** close the three causes behind every failure and the one
   truthfulness gap. Each is a single seam with a structural regression.
2. **D1 and E1** make the twin identical whenever the model behaves.
3. **A3** needs the owner's decision (wave-04 item 1).
4. With A1+B1+C1+D1, runs 01, 02, 04, 06, 07 and 08 would change as follows
   (projected, not measured):
   - 01, 02 and 08 lose the verb they were refused;
   - 04 must choose `report`;
   - 06 and 07 match the terminal's words, and their limits are drawn on the
     card.

   A re-measure of ten would show whether the projection holds.
