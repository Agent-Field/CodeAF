# W5-B — the chat-driven local-file journey, validated end to end

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
