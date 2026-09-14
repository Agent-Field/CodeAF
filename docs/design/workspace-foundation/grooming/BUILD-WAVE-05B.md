# W5-B — chat-driven local-file journey: fixes and remaining failures

## Round 2 — the journey as an acceptance driver (current; supersedes "done" below)

**Status: NOT marked done in this record.** The coordinator rejected completion at
live 2's 31/33. Round 2 was run in Fleet job `20260914-185234-000443`, which
resumed the session of parent job `20260914-182416-000442`. All times are UTC,
taken from `date -u` and the logs.

**Acceptance statement (unchanged in intent, stricter in checks).** One real-model conversation creates, edits (instructions, watch, report path), asks for a second folder, and pauses, resumes and stops. It is checked off `bin/aforge`'s records:
- **Failures are machine-visible.** Any failed check fails the Go test, and the runner exits non-zero (`cv.acceptance`). Only this scenario does this; the exploratory scenarios still only report.
- **The second-folder sentence says what the person wants:** "leave both folders where they are; if this cannot be watched as requested, explain and keep the existing setup". The run must end in one of two ways:
  - a watch that really reaches `inbox/` with its subfolders and `notes/`, and nothing unrelated, on a card the person approved;
  - or the setup exactly as it was, with the project tree byte-identical (no relocation), no broader watch, no replacement item, no workaround the person did not approve, and a reply about the unsupported request.
- **The person answers as that sentence reads.** The conversation is `Interactive` (the TUI's setting) and holds `WatchQuestions`, so a model's `ask` reaches the driver and is never decided by its own default. The person:
  - never takes the model's pick, and skips options that move, copy, link or broaden;
  - approves a card only when it truly watches both folders;
  - declines every consent in that turn.

**Driver corrections, each with its reason:**

| Correction | Reason | Assertion strength |
| --- | --- | --- |
| The instructions edit is judged on the fixture's fact: the digest's venue line (or the line after it) names Priya. The instructions or acceptance that stand must also name owners as a whole word (`instructions-ask-for-owners`). | Live 2's digest said "**Priya** to book the venue" and failed only on a literal `owner` label nobody asked for. The acceptance review then showed the fixture fact alone cannot prove the edit, hence the spec-side check, and whole-word matching so `markdown` or `known` do not count. | Stronger: a spec check was added. |
| An already-recursive watch is a truthful no-op (`already-recursive-watch-left-unchanged`: same id, version and glob), and the nested file must still wake a run whose changes name `inbox/clients/acme.md` and reach the report. | Live 2's watch was `inbox/**/*` from creation, and the model rightly changed nothing. | Stronger: the nested run's changes are now asserted. |
| The second-folder checks now require an unchanged tree, spec version, glob and brief; a card-approved widening that keeps subfolders; and a reply check. | Live 2 moved `notes/` under `inbox/` after the default dial answered the model's own ask. Live 1 changed the instructions as a workaround. Both would now fail. | Stronger. |
| The reply check accepts "two folders"/"both folders" in place of "notes", and more negation words. | Live 4's reply ("Can't watch two folders into one digest with a single file watch — brace expansion isn't supported …") was honest, with the tree and setup untouched, and failed only for lacking the word "notes". The review had predicted this false failure. The reply's content is a person's judgement; the tree, version and watch checks catch any workaround. | Relaxed on purpose, with this reason. Live 4 stays failed. |

**G3, a product failure found by live 3 and fixed.** The run for the nested file
answered `…processed it.\n\n<report># Digest …\n</report>`. The opening tag was glued to
the report's first line, so a whole report was withheld as `unopened-report`
(the previous report was kept, as the law says). The fix is in `delimitedReport`:
a `<report>` at the very start of a line, before any report has opened, opens
the report, and the rest of that line is its first line. A tag later in a line,
or after a report has opened, is still a mention, and a closing tag glued to words still
closes nothing. `TestAnOpeningTagGluedToTheFirstLineOpensTheReport` **fails at
`a3252e8f3`** ([receipt](validation/w05b-old-logic-a3252e8f3.log)). The manual's
report section and the change entry say so.

**Revisions, round 2:**

| Commit | What |
| --- | --- |
| `76f92b88d` | Acceptance driver: fail on any check, the second-folder sentence and checks, the person's answers (driver only) |
| `8ca78e406` | G3 parser fix; the question watcher (review blocker); tree snapshot; stricter approval, picker and reply checks; the owners spec check |
| `0ad0ccea7` | Second review's fixes: a glued tag opens only when nothing has opened; whole-word owners; the honest-reply wording. **Final runtime and driver.** |

**Live runs, round 2:**

| Run | Head | Time (UTC) | Result | Spend | Log |
| --- | --- | --- | --- | --- | --- |
| live 3 | `76f92b88d` | 18:56:23–19:00:38 | **34/35, EXIT 1**. Failed: `nested-file-woke-a-run-and-reached-the-report`. The run did wake on `inbox/clients/acme.md`, and was withheld `unopened-report` (G3). | $0.0383 | [log](validation/w05b-live-journey-run03.log) |
| live 4 | `8ca78e406` | 19:06:34–19:18:09 | **36/37, EXIT 1**. Failed: `the-reply-explains`, a driver false failure over an honest reply. All tree, relocation, replacement, broadening and setup checks passed, and G3's report published. | $0.0414 | [log](validation/w05b-live-journey-run04.log) |
| live 5 | `0ad0ccea7` | 19:18:31–19:23:36 | **21/37, EXIT 1**. It was the last retry this round allows. | $0.0306 | [log](validation/w05b-live-journey-run05.log) |

Total spend for the wave is **$0.1972** against the $0.50 limit.

**Live 5: why it failed, and the blockers that remain.** The combined journey is **NOT DONE**.
1. **Runs looped to their step limit (product and model).** Each of runs 000001–000004 was withheld `at-a-limit` with nothing published. Run 000001 made 20 tool calls, repeating `ls inbox` 6 times, `read inbox/a.md` 5 times and `ls reports` 3 times, and never replied with a report. The withholding is truthful: the previous report stayed, and the code was recorded. But a digest of one file should not take 20 steps. Live 1–4 at the same model had no such loop. The cause is not diagnosed: this chat's instructions ("Read all files in inbox/ recursively. If none exist, skip. …"), the step limit it ran under, or model variance. That is the next investigation, not a fix made here.
2. **The rename was an instructions edit, not a report edit (G2 not reliably closed).** "rename the digest file: keep it at reports/weekly-digest.md" revised the instructions to mention `reports/weekly-digest.md` while `does.report` stayed `reports/digest.md` (spec 3). The schema text fixed live 2's case in one sample, and live 5 shows it does not hold. A structural seam is needed, and none is chosen here: for example, the named-report refusal that already reads the person's sentence for a file could also apply to an edit whose sentence names a new report path.
3. **Driver cascade (a driver defect).** When the rename failed, the driver's terminal `standing add --report reports/weekly-digest.md` probe (meant to be refused) succeeded. It created a live item, and every later step judged that item: pause, stop, broadening and setup checks. The driver must stop any item its own probe created and keep judging the chat's item. So in live 5, the checks after the rename step are not evidence either way.

Live 5's second-folder turn left the project tree untouched and made no replacement of the chat's item. The `no-covert-broader-watch` and `existing-setup-kept-…` failures are the cascade's item, not the chat's.

**Acceptance reviews (independent, read-only Opus, no compiles).**
- **Of `76f92b88d`: not fit.** The one blocker was that a model's `ask` goes to question watchers, never the turn stream, so an `Interactive` turn would wait out its 5-minute limit. There were also five should-fix items: the Priya fact alone proves nothing; relocation is evadable by copy or link; the reply check was trivial; approval ignored subfolders; the picker could choose a move. All were fixed in `8ca78e406`.
- **Of `8ca78e406`: no blockers, fit to certify.** Its should-fix items were `own` matching `markdown`, the reply check failing honest replies, and a glued tag after a report reopening it. All were fixed in `0ad0ccea7`. Recorded, not changed:
  - a teardown log race in the watcher goroutine (tiny window);
  - the picker skipping a safe option whose words mention a move (it falls back to a reframe, which is safe);
  - the widened branch not reading the reply.

**Focused checks at `8ca78e406`** ([receipt](validation/w05b-focused-checks.log)), all PASS:
- `make build`, vet, `make test-laws` (no failures), `make test-packed-manual`;
- untagged `internal/e2e`, `TestLocalWorkJourney`, `TestChatDoorJourney`;
- `internal/standing`;
- the session families `Stand|Standing|Report|Publish|Journey|TestTheFixedPrefix|Card|Owner|Watch|Edit`;
- `internal/manual` and `make changelog-check`.

**At `0ad0ccea7`:** `go vet -tags e2e ./internal/e2e/`, and session `TestAnOpeningTagGlued|Report|Publish|Unopened|Unclosed|Parked`, PASS.

**Timestamps corrected.** The progress headings written earlier (18:40, 18:58, 19:50, 20:00) ran ahead of the clock. The logged times of round 1 are:
- base checks 18:27;
- live 1 18:30:33–18:33:43;
- live 2 18:41:41–18:45:10;
- final focused checks 18:49;
- lane pushed about 18:51.

---

## Round 1 (history)

2026-09-14. Lane `codex/personal-next-0914` (Claude Code Opus on Spark), base
`3788e569e` on `codex/personal-ai-backend` (the W5-A merge). Spark only,
`GOMAXPROCS=4 GOFLAGS=-p=2`. There was no full tui3, session or cmd suite, no full tagged E2E run, and no
`make check`. The lane was not merged, no pull request was opened, and nothing was pushed to dev, staging or main.
Quarantine: `/home/santosh/af-pai-integrate` was never read, merged or touched.

## Contract

Ongoing work set up, changed and ended from one conversation, on local files
only:
- create;
- edit the instructions, the watch and the report path;
- pause, resume and stop;
- handle a second folder asked into the same report truthfully.

Authority comes from persistent folders and explicit placements, never from a
navigation reference. Reuse `stand`, `standing_edit`, the owner record, the
watch, the session and the store. Add no compound triggers, no universal
graph or store, and no new product concepts. Never broaden or narrow a watch
silently. An honest question or refusal for a multi-folder pattern is
acceptable.

## Acceptance

`TestPAIChat` scenario `journey` (`internal/e2e/paichat_e2e_test.go`,
`scenJourney`) is one conversation with a real model
(`deepseek/deepseek-v4-flash`). Every run goes through the shipped `bin/aforge
standing check`, and every check is read from the binary's own records. A check passes when:
- the card names the report, the folder and the placement's rule;
- a reference is not a placement, and the placement's rule is kept while the reference's rule is absent in every report;
- each edit is the same item one version on, and reaches the next report;
- a moved report publishes at the new path, leaves the old file byte-identical,
  frees the old path (the terminal `add` there succeeds) and owns the new one
  (the terminal `add` there is refused);
- for the second folder: one live owner, no card claiming a folder the watch
  does not reach, inbox not dropped unseen, no silent broadening, an unwidened
  watch left as it was, and a run whose changes are exactly what the watch
  reaches;
- pause runs nothing, and resume catches up once and publishes;
- stop runs nothing and leaves the report as it was, and nothing else is left running;
- no run reads outside the project or is parked on a refusal.

## Revisions

| Commit | What |
| --- | --- |
| `454617ad5` | The live driver scenario `journey` (tested at live 1) |
| `e3201af9c` | **Source fix**: a file watch is said by its pattern; `does.report` says an edit moves it; manual, probes, change entry, four session tests (tested at live 2) |
| `a3252e8f3` | Review fixes: a words-only watch edit says why; manual wording for the conditioned case; comments; change entry; driver accepts an already-reaching watch. **Final runtime.** |

The runtime difference between live 2 (`e3201af9c`) and the final revision
(`a3252e8f3`) is one refusal string branch in `standEdit`, plus comments. That
branch is covered by `TestAFileWatchIsSaidFromItsPatternNotTheModelsWords`. No
live run was made at `a3252e8f3`.

## Gaps found, and how

| Gap | Found by | Cause |
| --- | --- | --- |
| **G1 — a file watch's `when ·` line could claim a folder its pattern never reaches.** `inbox/*` with `when_words` "whenever something lands in inbox/ or notes/" drew exactly those words on the card, in the stand result (so the model's reply repeats them), on home and in `standing list/show`. | A scripted probe before any live call. It was not observed live: neither live model sent `when_words` for the watch. | `When.CardWords` returned the model's words for any unconditioned watch. The chat door's round-1 choice was that "the model's own words still win". |
| **G2 — "rename the digest file" became a permanent stop and a new card.** The model's own words: "Need to move the `does.report` path — that means a new standing item since the path is part of the key." Nobody asked for a stop, and the reply also misquoted the costs. | Live 1 | `stand`'s schema told the model that `placement` can change on an edit, and said nothing about `does.report`. The edit itself always worked, as the new session regression proves. |

Where the scope was already right, the slice adds evidence, not code:
- A chat edit of the report path moves the owner and publishes at the new path. It is live in both runs (the second as an edit), and covered by `TestAReportPathChangedInTheChatPublishesThereAndFreesTheOldPath`.
- A chat edit of the watch works (live 1, and `TestAWatchChangedInTheChatReachesItsSubfolders`).
- The brace refusal holds, and one live owner holds at both doors.
- Placement governs and a reference does not.
- Pause, resume and stop behave as specified.

## Change → effect → evidence

| Change | Effect | Evidence |
| --- | --- | --- |
| `standing.When.CardWords`: an unconditioned file watch returns `WatchWords(glob)` | Every surface says `when inbox/* changes`, including an item recorded earlier with a model's words. A conditioned watch is unchanged. | `TestAFileWatchIsSaidFromItsPatternNotTheModelsWords` **fails at `3788e569e`** ([receipt](validation/w05b-old-logic-3788e569e.log)). `internal/standing` whole package PASS. tui3 `CardWords\|Conditioned\|Standing\|Stand` PASS. |
| `standingItem` and `standingEdited` ignore `when_words` on a file watch; a words-only edit answers `nothing to change: a file watch is said by its pattern, so when_words change nothing on it — to change what wakes it, send when.glob` | The record matches the terminal's twin. No `changes · what wakes it` card can have an invisible change. | Same test (propose, record, words-only edit). Two older pins changed on purpose: `TestAWatchWithNoWordsOfItsOwnSaysWhenItWakes` and `TestAnEditCardSaysTheOldAndTheNewAndKeepsTheRest`. |
| `stand` schema: `does.report` "On an edit, the path it moves to: …"; `when_words` "A file watch is said from its glob, never these." | The rename is an edit of the same item. | `TestTheReportFieldSaysAnEditMovesIt` **fails at `3788e569e`**. Live 2: `stand` edit, `report · reports/digest.md … → reports/weekly-digest.md …`, spec 3, same id; the old path is released and the new one owned. One live run is one sample, not a rate. `TestTheFixedPrefixStaysUnderItsBudget` PASS. |
| Manual `standing-orders`: *Nothing stands until you say yes* (the when line), *Two orders on one report* (renaming or moving the report; two folders into one report) | The chat can answer "rename the report", "watch two folders into one report" and "why the glob and not my words". | Three probes added. `internal/manual` and `make test-packed-manual` PASS. Live 2's model quoted the new two-folder paragraph. |
| Journey coverage, not regressions | — | `TestAReportPathChangedInTheChatPublishesThereAndFreesTheOldPath` and `TestAWatchChangedInTheChatReachesItsSubfolders` pass at the base too. |

## Live runs

Both runs were serial, driven by `scripts/pai-chatvalidate.sh journey N` with
`PAI_ROOT=/tmp/af-pai-next-0914/live` and `PAI_BUDGET_STOP=0.45`. Total new spend
was **$0.0869** against the $0.50 limit.

| Run | Head | Result | Spend | Log |
| --- | --- | --- | --- | --- |
| live 1 | `454617ad5` | **29/33** | $0.0388, 47 calls | [log](validation/w05b-live-journey-run01.log) |
| live 2 | `e3201af9c` | **31/33** | $0.0481, 56 calls | [log](validation/w05b-live-journey-run02.log) |

**Live 1's four failures:**
- **G2** accounts for three: `same-item-one-version-on`, `report-path-moved` and `next-run-publishes-at-the-new-path`. The last one read the stopped item, because the driver followed the successor by report path.
- **`declined-leaves-the-watch-as-it-was`** was a driver check that was too strict. After its brace pattern was refused, the model kept `inbox/**/*` and changed the instructions to also read notes/ on each run. The card drew that change, and the reply said "a notes file alone won't wake it". The check was replaced by `unwidened-watch-left-as-it-was` (the glob and the item are unchanged).

**Live 2's two failures stay failures:**
- **`edit-reached-next-report`: run variance.** The instructions edit landed (spec 2), and the next digest listed "**Priya** to book the venue" but never the word owner, owns or owned. Live 1 passed this check.
- **`J-edit-watch same-item-one-version-on`: driver false failure.** The watch was already `inbox/**/*`, and the model correctly said "Nothing to change". The driver now accepts a watch that already reaches the subfolders. That change was not re-run live.

**Live 2's second-folder turn, recorded and not scored as success.**
1. The model tried braces, which were refused.
2. It edited the instructions to read notes/ as well.
3. It called `ask` with a choice. The driver attaches no question surface, so the session's dial answered the model's default (`merge (default · nobody to ask)`).
4. The conversation then ran `bash mv notes inbox/notes`, which the chat's ordinary approval rules allowed with no consent event, and said so in its reply.

A person at the TUI would have seen the question. Moving the person's folder
to fit a watch is not something this slice makes truthful or safe. It is the
chat's general approval posture on workspace files, and it is recorded in the
limits below.

## Focused validation

All on Spark ([receipt](validation/w05b-focused-checks.log)).
- **Base `3788e569e`:** `TestLocalWorkJourney` + `TestChatDoorJourney` PASS; the standing and session edit/owner families PASS.
- **Final `a3252e8f3`:**
  - `make build` (`aforge a3252e8f3`);
  - vet for standing, session and cmd, plus the e2e vet;
  - `make test-laws` (no failures), `make changelog-check` and `make test-packed-manual`;
  - untagged `internal/e2e`, and both deterministic journeys;
  - the whole `internal/standing` package;
  - tui3 `CardWords|Conditioned|Standing|Stand`, cmd `Standing|Stand|Owner|Watch`, and session `Stand|Standing|Edit|Owner|Watch|Card|TestTheFixedPrefix|Journey`.
  
  All PASS.

## Independent review

A read-only Opus review of `e3201af9c`, with no compiles, found **no blockers**. Its findings and what became of them:
- **Fixed in `a3252e8f3`:**
  - should-fix: the manual overclaimed for a conditioned watch;
  - should-fix: a words-only edit got a bare "nothing to change";
  - nit: stale fallback comments;
  - nit: the change entry dated the report edit wrongly;
  - nit: two journey tests were labelled as regressions.
- **Recorded, not changed:** `standingNamed` still matches an old item by a `When.Words` no surface shows any more. It is harmless.

## Limits (stated, not hidden)

- **The chat's stop asks no card.** A model that misreads a sentence can still stop work permanently. G2 removed the cause seen live, not the possibility. Whether a stop from the chat should be confirmed is an open decision for the owner, not taken here.
- **Two sibling folders cannot be watched into one report.** There are no braces, and a pattern over both reaches the report's own folder. The honest outcomes are a question, or one watch whose runs also read the other folder, which never wakes the work by itself. A live model may also restructure the person's files under the chat's approval rules (live 2).
- **The driver answers questions by the default dial, not as a person.**
- **Model-written `when_words` still stand for moments, rhythms, idle waits and probes.** Only a file watch is said from its record.
- **G1 was fixed on scripted evidence.** No live run exercised a model sending `when_words` for a watch.
- Two live runs are two samples, not a pass rate. There was no tui3 surface run, no tmux run, no timer install, no connector and no Slack.

## Demo

```sh
make build
mkdir -p /tmp/pai-journey && go test -tags e2e -c -o /tmp/pai-journey/e2e.test ./internal/e2e/
OPENROUTER_API_KEY=... PAI_ROOT=/tmp/pai-journey PAI_BUDGET_STOP=0.10 scripts/pai-chatvalidate.sh journey 1
# kept home, project and log: /tmp/pai-journey/journey/run01/{home,project}, /tmp/pai-journey/logs/journey-run01.log
AFORGE_HOME=/tmp/pai-journey/journey/run01/home bin/aforge standing list
```

Each run costs about $0.04–$0.05 and takes about 3 minutes. It uses a disposable `AFORGE_HOME`, never `~/.aforge`, and installs no timer.
