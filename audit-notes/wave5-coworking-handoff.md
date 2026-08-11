# Wave-5 handoff: the co-working campaign's asks, compiled

Compiled 2026-08-10 by the Wave-5 handoff lane, at `chat-v2` HEAD `0b94a83`.
Read-only except this file and one ledger paragraph (13.9) pointing at it.

**Who this is for.** The co-working campaign owns `internal/resident`,
`internal/exec` and `internal/plan` (Part 13 non-blockers; it merges onto
chat-v2 rebasing over it). Wave 5 — resident-side per-task orchestrator loops
(4.6 v2, 11.2) — cannot start until that boundary lifts. Throughout the chat-v2
campaign, lanes filed precise asks against that territory and scattered them
across ledger sections 12.x/13.x. This document is all of them, in one place,
each verified against the code at HEAD with file:line citations, ordered by
unblocking value for Wave 5.

**Working-tree caveat.** Two lanes were in flight uncommitted while this was
compiled: a head lane citing a future ledger section "13.6"
(`internal/head/urgency_test.go:189`, uncommitted) and a surface lane closing
13.8 finding 1 in `internal/tui2/chat/rooms.go`/`scope.go`. Citations below say
which tree they were read from; everything else is HEAD.

## Summary table

| # | item | owner half needed | size | ledger source | risk if skipped |
|---|------|-------------------|------|---------------|-----------------|
| H1 | `CommandHeadInterrupt` reconciler arm | resident: one `case` + one seam | ~10 lines | 12.8.10, 12.9.7 | every journaled stop is REJECTED with a visible "not supported" receipt beside a stop that happened |
| H2 | spine root permanently `status='running'` | store: one status-model decision | 1 decision + small diff | 13.8 finding 4 | rail and head contradict each other on one screen; every Wave-5 orchestrator board read inherits the lie |
| H3 | owner-pinning: kill the `LastSeen` singleton's last two tenants | resident: after the flip | small, sequenced | 9.7, 12.2, 12.3.6 | ownerless deliverables stay silent-but-not-lost forever; legacy arm never dies |
| H4 | room-policy pin: env plumbing retirement | resident + cmd | tiny | 12.3.7, 13.4 f.2, 12.12.8 | two packages spell one env constant; policy still env-sniffed at construction |
| H5 | per-call window-occupancy journaling | exec + store (+1 column) | small engine piece | 5.9, 13.3 unwired, 9-item verification below | ctx% (5.9 card, 10.5.23 strip) stays unbuildable; "why did it get dumber" stays unanswerable |
| H6 | worker-pool ceiling / no queue between jobs | exec/resident runner | PENDING from fan-out lane | 13.6 (not landed; test citation only) | prompt law already updated to match measured behaviour; ledger entry missing |
| H7 | Wave 5 proper: per-task orchestrator loops | resident (scoped below) | the wave | 4.6, 5.5, 11.2, 12.1.5, 13.4 | v1 phasing stays law; 5.3's "same command row" promise stays open |
| H8 | standing co-working notices (sweep) | plan/resident | report-only | 12.6, 12.9, 13.8 | known items get rediscovered |

---

## H1 — The resident interrupt arm (highest unblocking value, smallest edit)

**What.** `applyCommand` in `internal/resident/resident.go` gains the one case
that drains `store.CommandHeadInterrupt` rows, calling the head's
already-built, already-tested `ApplyInterrupt`.

**Why.** 12.8.10 built the whole seam head-side and stopped at the boundary:
"Neither file is this lane's: internal/resident is co-working territory."
12.9.7 landed the store half (`store.CommandHeadInterrupt`, commit `8ff2b4e`)
and re-stated: "The remaining edit is unchanged and still one line in
`internal/resident/resident.go`'s `applyCommand`."

**State at HEAD.**
- Store half landed: `internal/store/thread.go:186-196` (kind), `:1096`
  (validCommandKind), `:1170-1173` (isGlobalCommand — targets no node).
- Head half landed: `internal/head/interrupt.go:91-111` (`RequestInterrupt`:
  journals for the record, stops in-process for the effect), `:121-126`
  (`ApplyInterrupt(command store.Command) bool` — idempotent, reports whether a
  turn existed). The belt `interrupt` tool calls it at `interrupt.go:134`.
- Resident half missing: `applyCommand`'s switch (`resident.go:1055-1094`) has
  no arm; the insertion point is after `case store.CommandHandover` at `:1085-1086`.

**One correction to 12.8.10's sketch.** The sketch reads
`case store.CommandHeadInterrupt: return h.head.ApplyInterrupt(command), nil`
"where the resident already holds the head it serves". At HEAD the `Reconciler`
struct (`resident.go:204-227`) holds NO head, and it cannot grow a typed one:
the head imports the resident (`internal/head/head.go:23`), and
`resident.go:1497-1501` states the dependency runs one way only. So the honest
edit is one case plus one func seam in the existing `With*` builder idiom
(`resident.go:325-489`):

```go
// builder, beside WithCraftRunner:
func (r *Reconciler) WithHeadInterrupt(apply func(store.Command) bool) *Reconciler {
    r.applyHeadInterrupt = apply
    return r
}

// the case, before default::1087:
case store.CommandHeadInterrupt:
    if r.applyHeadInterrupt == nil {
        return commandOutcome{status: store.CommandRejected,
            result: "no head is serving this store"}, nil
    }
    if r.applyHeadInterrupt(command) {
        return commandOutcome{status: store.CommandApplied, result: "turn stopped"}, nil
    }
    return commandOutcome{status: store.CommandApplied,
        result: "nothing was running — the turn had already ended"}, nil
```

`cmd/aforge/chat.go` (which holds both objects) wires
`.WithHeadInterrupt(head.ApplyInterrupt)`. "Nothing running" is `Applied`, not
`Rejected` — `interrupt.go:118-123`: finding the turn already gone "is a true
resolution rather than a second cancellation".

**Evidence it's needed.** Not merely an undrained row: `applyCommand`'s
`default` arm (`resident.go:1087-1093`) REJECTS unknown kinds with a visible
receipt — `command kind "head_interrupt" is not supported`. Since `40c4915`
the belt `interrupt` tool journals on every use (`interrupt.go:99-101`), so
today each such stop produces a rejection receipt contradicting a stop that
actually happened. `orchestrator_test.go` (12.8.12) already covers "both
interrupt roads and the reconciler arm" from the head side.

**Risk if skipped.** The lens law (Part 3 / decision 7) stays broken for stops:
a headless or cross-process caller has no working escape key; the journal
carries stop rows the product visibly refuses.

---

## H2 — The spine root is permanently `status='running'` (one decision)

**What.** Decide the spine node's status model: either the root gets an inert
status (or a structural flag consumers can dispatch on), or "filter
`store.RootID`" is promoted from per-consumer habit to law.

**Why.** 13.8 finding 4 (live-use lane): rail says
`1 running · ◐ Permanent Aforge spine · 1 part running` while the head says
"Nothing is running right now — the board is empty", same frame. "Cause is
pinned, not guessed: the `nodes` row `id='root'` … is permanently
`status='running'`, the rail's live-work query counts it and the head filters
it. One of the two has to change." 13.8 marks it **surface + prompt, one
decision** — and the decision is the store's, because Running-at-root is
structural, not accidental:

- `internal/store/store.go:673-704` — `ensureSpine` INSERTs the root with
  `Running` (`:702`).
- `internal/store/store.go:746-756` — `applySpineRepair` actively RESTORES
  `status = Running` on open-time repair and replay. Any consumer-side fix
  fights this function forever.

**State at HEAD.** The head filters root at every read
(`internal/head/class.go:217`, `correction.go:279,298`, `revision.go:85,118`,
`attribution.go:42,68,73`). The rail's home count at HEAD
(`internal/tui2/chat/scope.go`, loop at ~`:375-385`) counts every
`Running/Claimed/Pending` node with no root filter; an uncommitted surface-lane
diff was adding `store.RootID` guards to `scope.go` while this was compiled —
the surface half may land as a workaround before the store decision is made.

**Exact ask.** A store-side decision, stated in the ledger when taken: either
(a) the spine's aliveness stops being spelled through the same `Status` enum
work uses (new inert value, with `applySpineRepair` and replay updated — a
migration/rebuild concern), or (b) the store publishes one query/predicate
(e.g. `Snapshot` marks the root, or a documented `IsSpine`) so no consumer
hand-rolls the filter. (b) is the cheap honest floor; (a) is the clean one.

**Evidence.** `resident_test.go:31` asserts the graph context string
`root | Permanent Aforge spine | running` — the current model is pinned by
co-working tests, which is exactly why chat-v2 lanes could not touch it.

**Risk if skipped.** Every Wave-5 orchestrator's board read re-litigates the
filter; a permanently-running mystery job "with a forever-climbing clock is
also just noise on a new user's first screen" (13.8.4). Trust failure class of
13.3 bug 3.

---

## H3 — Delivery/question re-homing → owner pinning: what landed, what remains

**What.** Verify state: 9.7's ask ("deliveries pin to the OWNER thread, and
re-homing dies together with the `LastSeen` singleton") is mostly LANDED
resident-side. Two tenancies remain.

**Landed at HEAD** (Wave 0 batch 2, behind the 12.2 policy seam):
- `internal/resident/resident.go:1961-1983` — `roomPolicy`
  (`roomsLegacy | roomsOwnerPinned`), selected once at construction
  (`New`, `:322`).
- `resident.go:2010-2029` — `deliverySessionID` returns the owner room under
  `roomsOwnerPinned`; ownerless origin falls back to the legacy `LastSeen`
  route (12.3.6's corrected shape: "the choice is between the wrong room and
  no room, and no room is silence").
- `resident.go:2038-2044` — `announceRoom`, the single resolution point 12.3.7
  named.
- Questions too: `internal/resident/questions.go:222-281` — the orphaned
  blocking-question rescue asks `pinnedQuestionRoom` (`:281`) first, so under
  rooms the task's own thread adopts, not whoever is sitting there.

**Still needed from the co-working side.**
1. **The `LastSeen` singleton's death is gated on a home room.** 12.3.6:
   "The singleton fully dies only when a system/home room exists to own the
   ownerless." Wave 5 (or the 5.24 homes completion) must give ownerless
   deliverables an owner; until then `store.LastSeen()` reads at
   `resident.go:2020` and `questions.go:223` stay load-bearing.
2. **The legacy arm dies one wave after the default flips** (12.2: "The legacy
   path dies with the old chat, one wave after the flip") — delete
   `roomsLegacy` and both fallback reads then, not before.
3. **The 5.3 letter.** 13.4's journey table: "in-room speech journals a steer
   row, not the same command row 5.3 promises; legal under 4.6 v1 phasing,
   letter closes in Wave 5." Owner pinning is the delivery half; the speech
   half is H7.

**Risk if skipped.** Delivered→owner stays env-gated forever (13.4 J5.5), and
an ownerless overnight deliverable is permanently "silent-but-not-lost on the
board".

---

## H4 — The room-policy env pin: closed, plus two lines of debt

**What.** 13.4 finding 2 ("half-pinned policy config": `--v2` flag without the
env left `resolveRoomPolicy` on the legacy arm) is **CLOSED** by `5cb0f01`:
`cmd/aforge/chatv2.go:107` `os.Setenv(chatV2Env, "1")` before anything that
reads it is built (12.12.8, "internal/resident untouched"). Note 13.5's "the
finding is still open" was measured at `1a1bb78` and is stale.

**Remaining resident-side cleanup, small and deliberate.**
1. **One constant, two spellings.** `chatV2Env = "AFORGE_CHAT_V2"` is declared
   in both `cmd/aforge/chatv2.go:40` and `internal/resident/resident.go:1976`.
   Two packages holding one wire string is the exact failure mode 12.9.7 names
   for constants ("two constants holding one string is how a kind quietly
   becomes two kinds").
2. **Policy by parameter, not by environment.** `resident.go:1959-1960` says
   the choice is "made once, at construction … never re-derived from the
   environment", yet `New` (`:322`) derives it from the environment, once. When
   the co-working campaign next touches `New`'s signature, take the policy as
   an argument (or a `WithRoomPolicy` builder) and let cmd own the env read —
   cmd already owns the flag/env negotiation (`wantChatV2`,
   `chatv2.go:53-58`).
3. **The variable itself is scheduled to die** "one wave after the flip"
   (12.3.7: "operator plumbing … not a settings row").

**Risk if skipped.** Low today (the Setenv covers it); becomes real the day a
second embedder builds a Reconciler without the cmd path — tests already do
(`New` in tests gets `roomsLegacy` silently).

---

## H5 — Exec per-call window-occupancy journaling (one row per provider call)

**What.** Executors journal usage once per PROVIDER CALL, carrying (or
accompanied by) a window-size high-water mark, so a surface can compute ctx%
per conversational surface instead of an upper bound.

**Why.** 5.9 (doc lines 494-499): "context is per conversational surface — the
card's ctx% is the orchestrator's window; each worker row shows its own …
usage rows already journal per-request tokens; executors must also journal
window-size high-water marks (new, small)." 13.3 (unwired list): "ctx% needs
the window high-water journaling (5.9 note — small engine piece, still
unbuilt)."

**Honest sourcing note.** No landed 12.x/13.x ledger entry enumerates "the
three aggregate spine usage sites" in those words; the enumeration below is
this lane's own verification at HEAD. The 5.9 claim that "usage rows already
journal per-request tokens" is true only on SOME paths, which is precisely why
ctx% is currently an upper bound:

- **Per-call (correct already):** every structuring call through the pool
  bills one row — `internal/provider/pool/pool.go:113-136`
  (`recordStructuringSpend`, one row per `CompleteWithMessages`).
- **Aggregate per execution (the worker gap):** a worker run's tokens are
  summed across ALL its turns (`internal/exec/swe.go:976-983`, surfaced at
  `:296`) and journaled as ONE row per node
  (`internal/resident/runner.go:735-748`, `recordSpend`). A per-worker ctx%
  computed from that row's `prompt_tokens` is the sum of up to N windows —
  an upper bound, not an occupancy.
- **Aggregate against the spine (the head/root gap), three sites:**
  `cmd/aforge/run.go:173-180` (all headless preparation calls, one `RootID`
  row), `cmd/aforge/run.go:312-319` (the entire scheduler run —
  `scheduler.Usage()`, `internal/exec/schedule.go:105-118` — one `RootID`
  row), `cmd/aforge/chat.go:2870-2894` (`journalPlanSpend`, multiple plan
  passes summed into one `RootID` row). Because per-call head rows and these
  multi-call sums share `NodeID = store.RootID` (plus a cost-only voice row,
  `internal/command/command.go:343-348`), "the latest RootID row" is not the
  head's window either — the head's own ctx% is equally an upper bound.

**Exact change.** (a) `store.NodeUsage` (`internal/store/usage.go:37-44`) and
the `usage` INSERT (`usage.go:1086`) gain a window/context-length or
high-water column — coordinate with the co-working campaign's own F2 note
(`audit-notes/prompt-context-cache-audit.md`: "`cached_tokens` computed three
times, persisted nowhere … store/usage.go is outside the chat session's dirty
set — coordinate at merge"), which wants a column in the same table. (b) The
exec loop journals per call (or at minimum journals the run's high-water
window beside the aggregate). (c) The three RootID aggregate sites either move
to per-call journaling through the pool seam they bypass, or their rows are
distinguishable (they already carry no model on two of the three).

**Evidence.** 13.3: v2 status line ctx% blocked on exactly this. The
uncommitted surface-lane diff to `internal/tui2/chat/scope.go` adds a comment
block naming both missing reads ("PER-SURFACE CONTEXT … until they do there is
nothing to read. No gauge is drawn anywhere on this rail for that reason") and
a second read this campaign wants when the store can answer it: per-worker
money ("one read shaped like TopLevelJobUsage keyed by node, for one
subtree").

**Risk if skipped.** 5.9's card telemetry and 10.5.23's context cell stay `—`
forever; the "honest answer to 'why did it get dumber'" never exists; Wave-5
orchestrator health (amber past compaction) has no data source.

---

## H6 — Worker-pool ceiling / scheduling truth — PENDING FROM FAN-OUT LANE

**What.** The fan-out probe lane measured runner scheduling behaviour and
updated the head's prompt law to match; its ledger entry ("13.6") is **not
landed** — 13.7's header note records that a concurrent head lane "had already
cited a 13.6 of its own in `internal/head/urgency_test.go` before either
section was appended". At compile time that citation exists only in the
UNCOMMITTED working tree (`internal/head/urgency_test.go:189`):

> "the runner claims every ready leaf it has a slot for, so a second job
> commissioned while a first one runs starts beside it rather than after it.
> The receipt is still forbidden to promise a time; what it may no longer do
> is invent a queue."

**What is verifiable at HEAD.** The measured behaviour matches the code:
`internal/resident/runner.go:85-97` (`NewRunner … at most workers nodes
concurrently`, `slots: make(chan struct{}, workers)`), `:303` ("Tick claims as
many ready nodes as free slots allow"); headless twin
`internal/exec/schedule.go:84-88` (`concurrency`, default 8) and `:163-168`
(claim gate plus the host-load gate the ceiling cannot stand in for).

**The ask, stated as received.** Do NOT invent the fan-out lane's conclusions:
whatever it filed for exec (ceiling sizing, queue semantics between top-level
jobs, per-task claims for Wave-5 orchestrators) is **pending from the fan-out
lane** until its 13.6 section lands in `chat-rebuild.md`. Wave 5's own stake:
per-task orchestrator turns will contend for the same slot pool as workers
(4.6 v2 prerequisite "resident-side scheduling of orchestrator turns"), so the
ceiling's owner should expect that ask to arrive with Wave 5.

**Risk if skipped.** The prompt law and the ledger diverge (a test cites a
section that does not exist); Wave-5 scheduling lands against an undocumented
ceiling.

---

## H7 — Wave 5 proper: what the per-task orchestrator loop needs from the resident

Scope compiled from 4.6 (siting), 5.5 (lifecycle: the diagram's "orchestrator"
is a role in v1 and "only in v2 … a resident-side loop of its own"), and 11.2
("Wave 5 — orchestrators (v2 of 4.6). Resident-side per-task loops;
`answer_question` with class gates; ephemeral asks; `read thread://`;
fork-conversation-into-task; headless parity closes the lens law").

**Prerequisites, status-checked.** 4.6 v2 names four; three are LANDED —
per-session cursors (12.1, `8c724a9`), `Command.Issuer` + subtree authority
walking UP from the target (12.3.1-2), per-task budget ceilings (12.1,
`2345389`) — plus the consent desk moved resident-reachable
(`internal/consent`, 12.6.1, closing 9.10). The fourth, **resident-side
scheduling of orchestrator turns, is the wave itself.** The head half of 11.2's
list also already landed: `thread` read (12.9.3), `fork` (12.9.4), ephemeral
asks (12.9.5), `answer_question` reading (12.8.9).

**Scoped resident-side work items.**
1. **The spawn arm.** 5.5: "main head spawn tool (mints session + orchestrator)".
   The head's `spawn` journals a splice; the resident must mint the task's own
   session (12.1.3: needs a `session_opened` journal event — "empty rooms
   cannot exist yet … Wave 3 prerequisite; deliberately not invented") and
   stand up a per-task loop keyed by it. Sessions/rooms substrate exists
   (sessions table, per-room cursors); the loop does not.
2. **Turn scheduling.** Orchestrator turns are resident-scheduled (4.6 v2),
   billed to the task's own budget ceiling (12.1.5), sharing the runner's slot
   discipline (H6). 12.1.5's three follow-ups bind here: the overrun-replan
   gate "must NOT defer on task rails until `PendingOverruns` is task-aware";
   whichever wave surfaces ceilings must land raise-consent interception in
   the same wave "or a stopped task is unresumable from chat".
3. **The splice.** Planning→Working ("subtree spliced", 5.5) issued AS the
   orchestrator: commands carry `Issuer task:<root>` and pass the landed
   subtree-authority check (12.3.2 — an untargeted splice from a task issuer
   is refused, so orchestrator splices are always targeted).
4. **Steer-mailbox dissolution.** 5.5: "The narrator, steering mailbox, and
   cancel-rethink sentinel all dissolve into this one agent." Today steering
   is `messages.node_id`-addressed mail the executor drains between turns
   (Part 2.9, `head/coalesce.go:103-107` — "a user message anchored to a node
   is mid-flight steering the executor consumes"); under Wave 5 a steer
   addressed to a task becomes the orchestrator's own inbox row, and the
   resident's `narrate`/`cancelRethink`/`redirect`/`sentinel` func fields
   (`resident.go:208-217`) dissolve into orchestrator turns. This also closes
   13.4's 5.3 letter (H3.3): in-room speech becomes "the same command row 5.3
   promises".
5. **Escalation upward stays the three-event budget.** 9.8's inverse policy:
   "a task orchestrator narrates its own subtree and never leaks upward" —
   `announceRoom` (H3) is the enforcement point.
6. **`answer_question` class gates.** 12.1.4: every existing producer is
   consent-bearing, "the class axis is pure capability until a producer earns
   the label" — Wave 5's autonomy grant requires resident/compiler producers
   to start labelling `QuestionInformational` where true (9-item 4's
   conservative default stays: unlabeled ⇒ consent ⇒ escalate).
7. **Headless parity closes the lens law.** 4.6 v2: resident siting "is what
   makes headless `aforge do` tasks get orchestrators for free"; H1 is the
   same law for stops.

**Risk if skipped.** None of Part 5's product (rooms with their own voice)
graduates from the 4.6 v1 collapse; the one head stays the only speaker for
every task.

---

## H8 — Sweep: standing co-working notices in ledgers 12.6-13.8

1. **`internal/plan` `TestRenderTerrainStaysUnderTheCap` FAILS ON LINUX**:
   mkdirs a 100-CJK-char name (300 bytes) over ext4's 255-byte limit. Filed
   three times (12.9 preamble, 12.12 preamble, 13.5.1) and in Part 13's
   non-blockers as "the world-grounded-planning campaign's file (co-working
   territory) — report it to them; do not silently fix". Still owed a fix in
   that campaign.
2. **Terrain wiring markers** for the world-grounded-planning handoff sit at
   `cmd/aforge/chat.go:2947`, `:3134` and `internal/revision/sentinel.go:39`,
   `:93` (12.6.7; Part 13 non-blockers). Note line drift risk: chat.go has
   been edited since; re-grep for the markers rather than trusting the numbers.
3. **`internal/revision` exists BECAUSE `internal/plan` is co-working
   territory** (12.6.1): plan-revision code that arguably belongs in
   `internal/plan` was extracted elsewhere to respect the boundary. A
   post-boundary merge may want to reconcile the two.
4. **Lock arrangement note for resident-side callers** (12.6.6):
   `revision.Sentinel` takes the already-wrapped `plan.Completer`;
   "a future resident-side caller supplies its own" — Wave 5's orchestrator
   revision turns must bring their own lock discipline.
5. **12.8.5 cap-table note**: standing compiler 800, revision voice 300,
   compiler 1000+2·len/3, delivery gate 400, remainder 400, retry-worker 200
   are "unchanged and NOT this lane's" — cap decisions on resident/plan-side
   calls belong to the co-working campaign.
6. **12.1.6 perf trap**: the subtree-spend query must remain a `CROSS JOIN`
   or the planner full-scans `usage` per admission check; `NodeModels`
   (usage.go) has the same shape off the hot path. Binds anyone touching
   usage queries for H5.
7. **13.8's harness correction**: pyte lacks CSI S; any co-working lane
   replaying v2 byte streams must use `harness/render_live.py`'s SU/SD
   registration or it will report transcript corruption that does not exist.

---

## Suggested sequencing

1. **H1 first, alone** — ten lines, fully specified, its absence produces
   user-visible false rejections today, and it is the template edit for every
   command kind Wave 5 adds (12.3.3: "any wave adding a command kind must edit
   resident.go; … consider a dispatch seam if a third arm ever wants in" —
   Wave 5 is that third arm; take the seam decision here).
2. **H2 + H4 in one small resident/store pass** — both are single decisions
   with tiny diffs; H2 unblocks truthful board reads that every later item
   renders through; H4's parameter-not-env shape should ride whatever touches
   `New`'s construction anyway.
3. **H5 next, with the F2 column merge** — the schema change wants to land
   once; everything above it (5.9 cards, 10.5.23 strip, orchestrator health)
   is pure wiring after the column and the per-call rows exist.
4. **Wait on H6's ledger entry** from the fan-out lane before sizing ceilings;
   fold its conclusions into H7 item 2.
5. **H7 last, as the wave, in its own order**: spawn arm + session minting
   (needs H1's dispatch pattern), then turn scheduling (needs H6's answer and
   12.1.5's gates), then splice-as-issuer (all prerequisites landed), then
   mailbox/narrator dissolution (closes 13.4's 5.3 letter), then
   `answer_question` labelling, then headless parity — which is the wave's
   done-condition (11.2: "headless parity closes the lens law").
6. **H3's remaining tenancies close themselves at the flip** — schedule the
   `LastSeen`/legacy-arm deletion one wave after the default flips (12.2),
   once a home room owns the ownerless (12.3.6).
