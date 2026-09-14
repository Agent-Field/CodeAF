# Working checklist — product journeys

**This is the single execution checklist for the personal AI workspace.** Rewritten
2026-09-14 around the product journeys (user: "our goal is indeed to get to all of the
product journeys and thats the whole point of backend", C27). Backend milestones appear
only as steps inside the journey they serve. They support journeys and never stand in for
them.

- Accepted behavior: [DECISIONS.md](DECISIONS.md). A step below never confirms a proposal.
- Coverage, what exists beneath each journey, and the unconfirmed recommendations:
  [PRODUCT-JOURNEY-MAP.md](PRODUCT-JOURNEY-MAP.md). The map describes; only this file tracks.
- Earlier checklist text, every T-ID, box and receipt, unedited:
  [NEXT-STEPS-HISTORY.md](NEXT-STEPS-HISTORY.md). Where each old item went is listed at
  the end of this file.
- Per-slice receipts stay in their BUILD-* records. This file names them and does not copy them.

## Legend and ticking rules

| Mark | Meaning |
| --- | --- |
| `[x]` | Delivered and tested on a named commit with a named receipt |
| `[ ]` **Working tree** | Code exists in an active lane or uncommitted/unintegrated tree; not accepted |
| `[ ]` **Backend partial** | An owner, library, terminal path or failed-acceptance path exists on the maintained branch; the step is not demonstrated end to end |
| `[ ]` **Not started** | No implementation. A design or recommendation may exist |

- A journey's first line, **Journey accepted**, is ticked only when its whole acceptance
  passes through the real surface (`bin/aforge` in a terminal on Spark, real stores,
  isolated profile), on an open-weight model wherever the journey calls one (C28). Its steps
  can be `[x]` while it is not. A journey needs no personal trial or routine approval to be
  accepted; the person's feedback is always welcome and never a gate.
- A backend or package pass is not TUI evidence. A TUI pass is not model reliability.
  A closed-model run is exploration and never acceptance (C28).
- Record every failure, with its pass count and spend. Never retry until green, and never
  state a completion percentage.
- All builds and tests run on Spark (C25). Broad tui3/cmd/tagged-E2E suites stay
  deferred until stated otherwise (C21). A deferral is not a pass.

## The loop every journey serves

**Talk naturally → retain useful understanding → organize it → connect it to relevant
work → act within instructions → explain the result.**

| Loop stage | Journeys that deliver it |
| --- | --- |
| Talk | Every journey. Ordinary unfiled chat stays useful with no setup (C02) |
| Retain | J3 (and the retained correction in J2) |
| Organize | J0 browse, J2 organize and correct |
| Connect | J1G explicit guidance and watch, J4 impact, J5 collaboration |
| Act within instructions | J1 ongoing work, J3 governing rules, J6 while away |
| Explain | J1 inspector, J4 explanation chain, J2 "why is this here" |

## Recommended order, and why

**J0 → J1 → J1G → J2 → J3 → J4 → J5**, with J6 designed alongside J1–J3. This follows
the user's proposed sequence (ongoing work, organization and correction, retained
decisions, then collaboration). J1G and J4 are inserted as recommendations. It is not a
recorded decision (C27 settles only that journeys organize the work).

1. **J1 first:** every later journey observes real runs, so runs must be dependable on an
   open-weight model.
2. **J1G** is the promised checkpoint 3. It shows today's explicit guidance and an
   explicit cross-folder watch in the same fixture. It is a stepping stone and **not**
   genuine collaboration.
3. **J2 before J3:** correcting organization introduces the inferred-vs-explicit record
   that J3 generalizes. Design the two provenance records together (map §4). J2's
   deterministic parts can run beside J1.
4. **J4 is enabling work** placed before collaboration. J5 exchanges findings that affect
   other work. Without the revisions each run consumed, a navigable cause chain and
   duplicate detection, a peer finding cannot be told apart from a decision. Its effects
   could not be traced or undone either.
5. **J5 last among the core:** it needs provenance (J2/J3), correction that stays
   corrected (J2), authority (J3) and impact (J4) to already hold.
6. **J6 is within the confirmed goal** (C03, C17, C27). Its open part is an internal
   design task (DT3), not a new user decision.

## Current snapshot — 2026-09-14 (dated; replace, do not append)

- Maintained branch `codex/personal-ai-backend` at `0ac146768`. Draft #662 stays unmerged
  into dev (C07).
- Demo 1 (browse): runtime `36922486c`, worktree `/home/santosh/src/af-pai-demo-36922486c`,
  profile `/home/santosh/aforge-pai-demo-36922486c`. Pinned; later work does not touch it.
- W5-B chat-driven ongoing-work journey: **21/37, exit 1, not accepted**
  ([BUILD-WAVE-05B.md](BUILD-WAVE-05B.md)).
- Checkpoint 2, snapshot of the lane's own progress log at **2026-09-14T21:25:52Z**
  (Fleet job 450, from the lane's progress log only and not yet in a BUILD record; tree `/home/santosh/src/af-pai-experience-0914`,
  branch `codex/personal-experience-0914`):
  - cp2 source committed locally as `9687f15c5`, not integrated;
  - the fixture now pins open-weight models, with `z-ai/glm-5.3-flash` the lane's talk model;
  - an independent review was running;
  - terminal acceptance 1 had **started, with no result**.
  - Exploration on the default route `~deepseek/deepseek-v4-flash-latest` (resolved to
    `deepseek/deepseek-v4-flash-0731`) looped and never called `stand`. The lane
    classified this as a model limitation.
  - The earlier Haiku setup/pause/resume/edit/stop exploration (≈ $0.14) is historical
    and non-qualifying (C28).

---

## J0 — Browse, inspect, open and return (checkpoint 1)

*The person* walks Startup → Product/Marketing, inspects each kind of item, opens the one
shared chat from both folders, previews an artifact, returns to the same row, and still
starts an unfiled chat. Record: [BUILD-TUI-01.md](BUILD-TUI-01.md).

- [x] **Journey accepted** — real `bin/aforge` at `36922486c` in tmux on Spark, 48 PASS /
  0 FAIL, run ending 2026-09-14T20:28:55Z ([BUILD-TUI-01.md](BUILD-TUI-01.md),
  `validation/tui01-accept-checks.txt`). It covers browsing into Product and Marketing,
  opening the same shared chat from both with no new session and an unchanged transcript,
  back and return keeping path and row, work opened through its owners, an artifact
  previewed through the engine, and the unfiled chat reached from home. The pinned
  demonstration was delivered. Browsing calls no model.
- [x] Folders place: browse, reserved inspector, open by identity, return keeps path and
  row, wide and 60-column, keys and mouse — `36922486c`, tmux on Spark, 48 PASS / 0 FAIL.
- [x] Read-only `Collections.Page|Item|File` engine methods, including `--host`; an
  older engine is said, never drawn empty — `36922486c`, `collections_test.go`.
- [x] Personal fixture and pinned stable demonstration — `36922486c`, fixture log in BUILD-TUI-01.
- Ongoing and optional, **not blocking any journey:** when the person tries a pinned demo,
  append the date, revision, what confused them and the next smallest change to
  PRODUCT-EXPERIENCE-PATH.md. No trial has been recorded, and work continues without one.

## J1 — Dependable ongoing work, set up and steered from chat (checkpoint 2)

*The person*, in a chat placed in Product, asks that `reports/product-digest.md` be kept
current when `product/` changes. They answer the card, then inspect the work in Folders
(trigger, report, limits, state, how checks happen, rules reaching it, last runs and why).
They change a file, run one explicit check, and see state, run, cause and report update in
place. They pause (change ⇒ no run), start again (the pending change is observed), edit
instructions through the chat, rename the report through the chat, and stop (no run, no
restart).

**Checkpoint 2 is a bounded slice of J1, and its scope is frozen while it is in flight.**
Its contract is `BUILD-TUI-02.md` on the lane branch `codex/personal-experience-0914`, not
yet on this branch.
- **In cp2's scope:** chat setup through the card, the inspector, one explicit check,
  pause, start again, stop, and an instructions edit through the chat card.
- **Out of cp2's scope by design:** report rename and the rest of the W5-B chat journey
  (second folder, the step-limit loops).
- **When cp2 passes**, it ticks only the cp2 steps below. The J1 journey stays unchecked
  until the full acceptance holds.
- A closed-model run, including the earlier Haiku exploration, qualifies for neither.

**Checkpoint 2's bounded acceptance** (from its contract):
- a check after a change while paused admits no run;
- after starting again, the next run observes the pending change;
- after stop there is no run and no restart;
- the inherited Product rule's id appears in the run's rules check;
- the owner publishes the report with a receipt, and the stored path stays correct;
- serial runs on the pinned open-weight model, with the pass count and spend recorded and
  failures retained.

**Full J1 acceptance: everything above, plus the W5-B remainder:**
- the chat-driven journey including the second-folder turn passes (W5-B ended 21/37);
- a one-file digest does not run to its step limit;
- a report rename through chat changes `does.report`, or is refused honestly with the tree
  and item unchanged (G2).

**Reuses:** `standing.Store` (`Occurrences`, `Receipt`, `Running`, `SetStatus`, `Revise`),
the `stand` card, `workspaceview.Item`, the Folders place.

**Depends on:** J0.

- [ ] **Journey accepted** — Working tree.
- [x] Backend: local-file ongoing work created at the terminal — owner-published report
  with receipt, rules check, held report, pause/stop, restart after a kill:
  - focused 11/11 at `1c7012818` (source `c0de4d8f9`);
  - live6 `DEMO PASSED` on DeepSeek V4 Flash ([BUILD-WAVE-03.md](BUILD-WAVE-03.md)).
  - Serial pass rates on the same model: live8 7/10 at `84b423d87` (BUILD-WAVE-03), live9
    9/10 at `b87a4e756` ([BUILD-WAVE-04.md](BUILD-WAVE-04.md)), live10 10/10 at
    `1ad80ad3f` ([BUILD-ROUND-3B.md](BUILD-ROUND-3B.md)).
  - Not the chat-card path.
- [x] Backend: revision-aware pause/stop/edit writes cannot overwrite newer intent —
  `55bfc0118`, job `20260910-154640-000411`.
- [x] Backend: a file watch is said by its pattern (G1); a glued report tag opens (G3);
  brace patterns and duplicate live report owners are refused. G1 was fixed in W5-B round 1
  (its regression fails at `3788e569e`). G3 was fixed at `8ca78e406` and refined at
  `0ad0ccea7`. Focused checks passed at `8ca78e406` (`validation/w05b-focused-checks.log`).
  The refusals came from the W5-A merge `3788e569e`. All in BUILD-WAVE-05B.
- [ ] Backend partial — the chat-driven setup, edit, second-folder, pause, resume and stop
  journey: W5-B live 5 **21/37, exit 1** at `0ad0ccea7`.
- [ ] Not started — diagnose runs looping to their step limit (W5-B live 5, blocker 1).
- [ ] Backend partial — reliable report-path edit through chat (G2). Schema text alone
  did not hold. A structural seam is still to be chosen.
- [ ] Not started — acceptance driver stops any item its own probe created (W5-B blocker 3).
- [ ] Working tree — `WorkFacts` in the inspector: runs, receipt, report discrepancy,
  running mark, how checks happen. Folders `p` pause/start again, `s` stop, `e` edit in its
  chat. Manual sections. (Lane commit `9687f15c5`, snapshot above.)
- [ ] Working tree — the fixture pins open-weight talk, fallback, tier and vision models.
- [ ] Working tree — checkpoint 2's bounded terminal acceptance on the pinned open-weight
  model (started 21:25:52Z, no result recorded; passing it does not tick J1).
- [ ] Working tree — independent review of the cp2 source, and its fixes.
- [ ] Not started — integrate the accepted cp2 into `codex/personal-ai-backend` with its
  change entry, and pin a demo 2 without touching demo 1.
- [ ] Not started — model-written `when_words` for moments, rhythms, idle waits and probes
  are still shown as sent (only file watches are said from the record).
- [ ] Not started — v1 `aforge models` prints the old default route and ignores v3
  `model.talk` (noted by the cp2 lane as a misleading receipt; runtime receipts are
  `calls.jsonl`/`usage.jsonl`).
- Deferred from J1: timer installation (J6), a TUI "check now", a TUI instruction editor,
  compound triggers (later stage).

## J1G — Explicit guidance and an explicit cross-folder watch (checkpoint 3, stepping stone)

*The person* inspects one real scoped instruction and its scope, revises it, and sees the
next run held to the revision. They add a reference and see it does not become governing
placement. They watch the existing explicit Product → Marketing watch wake Marketing's
review, with its cause spelled as that explicit watch. **This is not semantic discovery and
not genuine collaboration (J5).**

**Acceptance:**
- the rule's id and version change in the run's rules check after the revision;
- the reference leaves the rules-reaching reading unchanged;
- the Marketing run's recorded cause is the watched file change;
- no idle rerun happens;
- serial runs on an open-weight model.

**Reuses:** scoped holds and `RulesReaching`, `collections add`/`place`, the standing file
watch, the Folders inspector.

**Depends on:** J1.

- [ ] **Journey accepted** — Backend partial.
- [x] Backend: explicit governing placements and folder-scoped holds, separate from
  references; read-only governing context across chat, workers, checker and scheduled
  runs (T12a/T12b) — `792a9dffc`, job `20260910-170155-000415`.
- [x] Backend: the inspector lists the rules reaching an item through the same
  `RulesReaching` reading `aforge standing show` uses — `36922486c`,
  `TestAnItemSaysWhereItIsFiledPlacedAndWhichRulesReachIt`.
- [x] Backend: a Product spec change wakes a Marketing review through an explicit file
  watch; the review is owner-published and checked against the Marketing rule; there is no
  idle rerun; a reference does not govern — wave 03, deterministic `TestLocalWorkJourney`
  with a scripted model, focused 11/11 at `1c7012818` (source `c0de4d8f9`),
  `validation/wave03-validate-final.log` ([BUILD-WAVE-03.md](BUILD-WAVE-03.md)).
- [ ] Not started — in the TUI fixture: revise a scoped rule through chat and observe the next run's rules check.
- [ ] Not started — in the TUI fixture: add a reference and show it is not placement.
- [ ] Not started — in the TUI fixture: the explicit Product → Marketing watch, with its cause labelled.

## J2 — Start unfiled, organize, correct, and the correction stays

*The person* chats with no folder. The assistant files the chat, or suggests a folder, with
a reason. The person sees who filed it and why. They correct it ("no, this belongs in
Product"), get a short explanation and undo, and a later turn rereading the same history
does not put it back. Folders are created, renamed, filed and moved from chat and from the
TUI. Filing never changes which rules apply. Placement follows only explicit direction.
Confirmed rules: C12, C15, C16, C23.

**Acceptance:**
- membership shows its origin and state;
- the transcript bytes are unchanged;
- the rules-reaching reading is identical before and after filing, unless placement was
  explicitly asked for;
- a rejected association is absent after rereading unchanged history;
- undo restores the prior membership exactly;
- an actual move changes folder-scoped guidance for future work only, keeping drafts,
  history and item-specific direction; a reference alone is not a move (C16);
- the shared chat is still one history.

**Reuses:** `workspace.Store` (`Create`, `Rename`, `Add`, `Remove`, placements), the
`collections` tool, `aforge collections`, the `Collections.*` engine seam, the Folders
place, and `stand` edit's placement move.

**Depends on:** J0. DT1 must be designed with J3's provenance.

- [ ] **Journey accepted** — Backend partial.
- [x] Backend: ordered membership, nesting, cycle checks and rename in the store; the
  terminal verbs `list/create/rename/show/add/remove/place/unplace/find`; the chat tool's
  `create/add/remove/place/unplace` — governing functional pass `792a9dffc`, job
  `20260910-170155-000415`; checkpoint 1
  shows filed vs placed and a reachable unfiled chat at `36922486c`.
- [ ] Backend partial — a `stand` edit moves ongoing work's placement on one yes
  (`bc42bb14c`). It was exercised only inside the failed W5-B journey.
- [ ] Not started — DT1: association provenance (who or what filed it, suggested / confirmed
  / rejected, reason, undo, rejection surviving an unchanged reread).
- [ ] Not started — a suggestion or filing step in a turn. At `0ac146768` the membership
  and placement writers found are `collections add`/`place` (chat tool and terminal) and
  placement inheritance at ongoing-work setup. No suggestion flow was found in
  `internal/session` or `internal/tui3`, and none has been demonstrated.
- [ ] Not started — rename from chat (the store and terminal have it; the chat tool does not).
- [ ] Not started — move as one operation (today it is remove plus add).
- [ ] Not started — TUI create, rename, file, unfile and move through those owners.
- [ ] Not started — "why is this here" in the inspector (origin and reason).
- [ ] Not started — C16 move-versus-reference acceptance through the product door.
- Deferred from J2: folder delete/archive (later stage, policy unresolved, DT4), bulk
  reorganization, discovery-driven filing at scale.

## J3 — Say a rule once; future work follows it, and a revision reaches the next run

*The person*, in Product's chat and with no save command, says "For Product's work, never
quote unreleased pricing". A clear instruction within the conversation's own direct scope
is kept on the spot as the person's receipt, with a visible row saying what was kept, where
it applies, and how to change or undo it. A wider or inferred scope, or the assistant's own
idea, stays a proposal. The rule appears in Folders with its source and scope. The next
run is held to it by id and revision. The person revises it, the next run uses revision 2;
they withdraw it, the next run does not use it. Out-of-scope work never receives it.
Confirmed: C09, C23, C24. The shape of the visible kept-rule receipt is DT2, unconfirmed.

**Plan:** the T03b one-record design's phased lanes 4–8. The design is the external
`pai-wave03-refs/design-t03b/DESIGN.md` (not in this repository), cited by
[BUILD-T03B.md](BUILD-T03B.md). Its §4.3 proposes the C09 capture rule. Existing holds keep
governing until its read cutover.

**Acceptance:**
- the E1 scenario test (chat, card and context-tool paths end as one governing record);
- the C09 boundary: a speculative suggestion and quoted text are not kept as direction;
- the run records the consumed direction id and revision;
- memory-off behaves the same;
- the rule survives restart;
- the inspector and `aforge standing show` agree;
- the live journey on an open-weight model has its pass rate reported (the external design names N ≥ 10).

**Reuses:** `internal/direction` (records, `PersonReceipt`, resolver, importer), holds,
`shared_context`, context_trace, occurrence records.

**Depends on:** J1 (dependable runs) and DT1.

- [ ] **Journey accepted** — Backend partial.
- [x] Backend: sourced, revisioned `shared_context` with explicit targets and withdrawal,
  informational only, refreshed per turn — governing functional pass `792a9dffc`, job
  `20260910-170155-000415`.
- [x] Backend: scoped holds govern chat, workers and scheduled runs, and a report is read
  against the rules that reached its run — `792a9dffc`; wave 03 (`c0de4d8f9`).
- [x] Backend library: lane 1 store compatibility and WAL; lane 2 direction schema v4 and
  write API with `PersonReceipt`; lane 3 resolver. Review blockers were fixed in
  `8ebfffc88` and `97a3023df`, and the re-review holes of `4e0c13013` closed after it;
  **no product caller** — Spark validation sections in BUILD-T03B.md.
- [ ] Not started — lane 4: the import tool, with a dry run on a copy of the owner's
  profile on Spark.
- [ ] Not started — lane 5: shadow resolve with a difference journal.
- [ ] Not started — lane 6: read cutover. The governing block and rules check read
  `Effective`; each run records the consumed direction revisions; one source for the bounds.
- [ ] Not started — lane 7a: write cutover (propose/note/propose_change, card, terminal
  verbs; the context tool writes findings).
- [ ] Not started — lane 7b: memory `decision` extraction routes to proposals.
- [ ] Not started — lane 7c: C09 statement capture, after its capture-trigger design.
- [ ] Not started — lane 8: export and retire the legacy hot path.
- [ ] Not started — T03b owner questions still pending: those in the external T03b design
  (not in this repository), plus a folder exclusion on an `everywhere` rule and override
  cycles (BUILD-T03B.md, *Open, recorded and not changed*).
- [ ] Not started — TUI: a rule's source, scope and revision history; revise and withdraw
  through chat; the next run observed.
- [ ] Not started — memory inspect/correct/withdraw tied to the work that used it (memory
  scope is only labels, and no record of which work used a memory was found at `0ac146768`).
- [ ] Not started — clause-specific exceptions and action-boundary re-admission (rest of T12d, D03).

## J4 — Change your mind: see what it affected and why (enabling)

*The person* revises a J3 rule. Folders shows the active work that will use the new
revision, and the completed outputs made under the old one. From an output they walk
output → run → why it ran → rules consumed → the chat message that set the rule. They
refresh one output as an ordinary run. The others stay marked and are never silently
overwritten.

**Acceptance:**
- staleness is derived only from recorded consumed revisions;
- every hop opens an existing owner record;
- a missing link reads `not recorded` and is never invented (C24);
- a published output is not rewritten without an ordinary approved run.

**Reuses:** occurrences, receipts, `parent_cause`, consumed-context records, direction
revision history, open by identity.

**Depends on:** J3 lane 6.

- [ ] **Journey accepted** — Backend partial.
- [x] Backend: occurrences record changes, phase, outcome, publication, rules check and
  instructions version; standing task runs name their occurrence as `parent_cause` —
  wave 03 (`c0de4d8f9`).
- [x] Backend: consumed-context records and the `context_trace` tool (T12c) — `792a9dffc`.
- [ ] Backend partial — tasks, forks and other executions still record `not_recorded`
  causes (T12d).
- [ ] Working tree — the inspector shows the last run's why, publication and rules check (cp2).
- [ ] Not started — "made under an older revision" for completed outputs.
- [ ] Not started — the navigable explanation chain in the TUI.
- [ ] Not started — who may refresh a stale output (D04).
- [ ] Not started — a revision reaching running work: refresh at a boundary, recheck
  before a consequential action (D03).

## J5 — Genuine collaboration: two efforts find a shared cause

*The person* has two independent efforts with **no explicit watch and no common folder**
(C04's "two bugs share one cause" is the software form). They meet the same underlying
problem. One consults the other, and the exchange is labelled with which execution spoke
and under whose authority. The finding is kept as a sourced finding, not a decision. Only
one investigation proceeds. Each keeps its own commitments and report. Only a choice
neither effort's instructions cover reaches the person. A sustained exchange may become a
discoverable shared chat. **The explicit Product → Marketing watch (J1G) never ticks this journey.**

**Acceptance:**
- origin and durable delivery receipts;
- the finding stays informational;
- no ownership transfer;
- exactly one investigation claim;
- stopping one effort does not stop the other;
- no new chat unless the exchange is sustained;
- serial runs on an open-weight model.

**Reuses:** direction findings (§4.1 rows for unattended and inter-folder writers),
`search_conversations` (reading, not coordination), task and standing owners.

**Depends on:** J2, J3, J4 and DT2.

- [ ] **Journey accepted** — Not started.
- [ ] Not started — origin-labelled consultation between executions, with durable delivery.
- [ ] Not started — investigation claim and duplicate suppression (D15).
- [ ] Not started — a peer finding is kept as a finding and never accepted without the person.
- [ ] Not started — bounded candidate discovery without constant global polling (D08).
- [ ] Not started — a shared chat's participation: admission, departure, and governing
  context for a turn in a chat placed in two folders.

## J6 — Work continues while away; come back to what needs you

*The person* closes the terminal. Ongoing work keeps checking. On return they see what
ran, what is waiting on their call, and nothing duplicated. Stop still stops, and an
action already under way is reported honestly. This is within the confirmed goal
(C03, C17, C27).

**Acceptance:**
- runs admitted with no window open, on the chosen background owner;
- held items shown on return;
- no duplicate occurrence after a restart;
- stop prevents later admission;
- effects already started are recorded, not claimed cancelled.

**Reuses:** the standing timer, restart and attempt records, held reports, the
`aforge standing check` exit codes.

**Depends on:** J1 and DT3.

- [ ] **Journey accepted** — Backend partial.
- [x] Backend: restart after a kill; an interrupted firing resumes as attempt 2 of the same
  occurrence; a held report waits on the person — wave 03 (`c0de4d8f9`), T13 in history.
- [ ] Backend partial — a background timer exists, but terminal-made items do not install
  it, and no journey here exercised it with the window closed.
- [ ] Not started — DT3: choose or reuse the existing background owner for v3 ongoing work,
  keeping the v3 and resident surfaces separate.
- [ ] Not started — on return, what needs your call, in the v3 surface.
- [ ] Backend partial — stop mid-run withholds aforge's report and note, but started
  effects are not cancelled (D06); failed runs are not retried.

---

## Internal design tasks — recommended work, not user decisions

Recommended defaults are in [PRODUCT-JOURNEY-MAP.md](PRODUCT-JOURNEY-MAP.md) §4. None is
confirmed. Each preserves existing authorized behavior until it is settled.

- [ ] Not started — **DT1** association provenance on membership (origin, state, reason,
  undo, rejection non-restoration), designed with J3's direction provenance. Serves J2, J3.
- [ ] Not started — **DT2** action-class policy: which writes need a yes and which are
  kept with a visible receipt and undo. The open owner decision on confirming a chat stop
  belongs here. Serves J2, J3, J5, J6.
- [ ] Not started — **DT3** background owner for work while away (reuse, not a new runtime). Serves J6.
- [ ] Not started — **DT4** folder delete/archive policy and what happens to contents,
  placements and rules. Serves later-stage folder management.
- [ ] Not started — **DT5** multi-root watch before any Boolean trigger composition.
  Serves later-stage triggers (W5-B's two-folder refusal).
- [ ] Not started — **DT6** red tests outside `.github/known-red.txt`: the `cmd/aforge-demo-home`
  tests (`deps-weekly` `{{evidence}}`) and `TestTheOpeningHintNamesBothDoors`, seen red at
  the base, need a defect report rather than silence.

## Later stage — covered by the goal, not required for J0–J6

- [ ] Not started — complete folder management: delete/archive (DT4), sharing explained,
  retention of folder history.
- [ ] Not started — optional folder purpose as description (never an automatic goal), and a
  bounded activity rollup.
- [ ] Not started — rich triggers: multiple sources, combined conditions, new occurrence
  versus continuation, missed runs, overlap, reporting policy (D05, D14).
- [ ] Not started — reusable methods separated from private inputs, grants and obsolete
  assumptions; run version and feedback (D09).
- [ ] Not started — recovery contracts: admission, cancellation, uncertain-effect
  reconciliation (T11c, D06, D17).
- [ ] Not started — narrow effective permissions and explicit host, account and worktree
  binding; copies never copy grants (D16).
- [ ] Not started — budgets across items and conflicts between works writing the same thing.
- [ ] Not started — deletion and retention across memory, direction, context and artifacts
  (artifact identity is an absolute path today).
- [ ] Not started — optional persistent identities spanning duties (D12, T02b).
- [ ] Not started — adapters beyond local files (the Slack journey was withdrawn, T13) and
  extension contracts (D18).
- [ ] Not started — discovery across all work at scale (beyond J5's bounded signal).

## Standing constraints and deferrals

- Nothing merges into dev, promotes or releases (C07). #662 stays a draft.
- Spark only for compilation and tests (C25). Claude Code Opus implements (C26).
- Not run for checkpoint 1 and still owed before any broader claim: the full tui3 package,
  full `cmd/aforge`, the tagged E2E package, `make test-remote`.
- Not run in W5-B: full tui3/session/cmd suites and the tagged E2E package.
- The rules check is a model's reading of a report, not enforcement.
- Deployment boundary: standing documents upgrade on write (schema 4 now), so old
  engines and tickers must be stopped and restarted together. No live installation or
  migration of the person's state has been performed.
- Quarantine `/home/santosh/af-pai-integrate` is never part of an integration.

## Where each earlier item lives now

| Earlier item (see NEXT-STEPS-HISTORY.md) | Now |
| --- | --- |
| T01a, T01b, T02a, T03a, T09a, T10, T11a, T11b1, T11b2, T12a, T12b, T12c, T13; the "Completed" section (architecture consolidation, 23-journey walkthrough, critical review, jobs `…000406`/`…000407`); baseline review; draft consolidation | Done; evidence kept in history; cited in J1, J1G, J3, J4, J6 where relied on |
| 2026-09-10 decision-session order table | History only; its open rows continue as D-IDs in DECISIONS and as DT1–DT6 here |
| Handoff to backend task `01a08ba9…` and its "acknowledgement pending" note | Answered: PRODUCT-EXPERIENCE-PATH, *Backend acknowledgement* |
| Deployment boundary of the first functional slice | Standing constraints above |
| T02b (D12) | Later stage: identities |
| T03b (D01/D02) | J3 lanes 4–8 and owner questions |
| T04 (D03/D06/D13) | J4 (running work), J6 (stop), later stage recovery |
| T05 (D05/D14) | J1 (reporting), later stage rich triggers |
| T06 (D04/D07/D08/D15) | J4 (stale outputs), J5 (consult, discover, claim) |
| T07 (D09/D12/D16) | Later stage: reuse, identities, permissions |
| T08 (D11/D17/D18) | J3 lanes; later stage recovery and extensions |
| T09b | Each journey's acceptance contract, written before its lane starts |
| T11c | J6 and later stage recovery |
| T12d | J3 (exceptions, re-admission), J4 (causal handoffs) |
| T14 | J1G (explicit watch) and J5 (impact with no watch) |
| T15 | Later stage |
| W5-B items (combined journey, G1, G2, G3, refusals, driver, stop card, `when_words`, suites) | J1 and DT2 |
| TUI checkpoint 1 | J0 (accepted) |
| "The person tries checkpoint 1" | J0 optional, non-blocking feedback note |
| TUI checkpoint 2 | J1's bounded cp2 steps; J1 as a whole also needs the W5-B remainder |
