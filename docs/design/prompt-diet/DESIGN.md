# The prompt diet — strategy

2026-09-10. Read-only audit of everything the v3 conversation sends a model per request,
three Opus passes (page, tool block, per-turn dynamic context), Pi as the lean baseline.
Numbers are measured on the rendered widest page and the marshalled belt, not guessed.
Coordinated with the quick-task lane (aforge-v2-38, PR #811 + quick/choice), which owns
`task_quick.go`, `taskDescription`'s tail, `handoffFacts`, the routing lines of
`system.md` and the worker pages; it has the findings that touch those and is folding
them in. Everything else in the prefix is free to move.

## 0. What we are actually optimising

There are three different bills and they want different things.

| Bill | What costs | What matters |
| --- | --- | --- |
| Frontier, cached | a moved byte in message[0] re-prices the whole conversation cold; dynamic bytes at the tail are paid on every round | **stability** of the prefix, then transcript growth |
| Frontier, uncached rounds (a 69-request cell) | every prefix byte × every round | **size** |
| Local, 8k–32k window | window share; attention dilution; every extra instruction is one more thing to get wrong | **instruction count**, then size, then stability (llama.cpp reuses a stable prefix KV too) |

So the order of priorities is: never bust the prefix, then send fewer laws, then send
shorter laws. A diet that shortens prose but adds a turn-triggered section to message[0]
makes the frontier bill worse. That single constraint shapes everything below.

Where we stand (conversation shape, this repo):

| Piece | Bytes | ~tokens |
| --- | --- | --- |
| page, all facts present | 23,391 | 5,850 |
| tool block, 18 tools | 24,044 | 6,000 |
| CLAUDE.md quoted (8 KiB cap) + footer | 8,600 | 2,150 |
| **before the person types** | **~56,000** | **~14,000** |
| Pi, prompt + 4–7 tools | ~14,000 | ~3,500 |

Pi is small because it has seven verbs and no tasks, standing, memory, accounts, steering,
or media. The honest comparison is not "aforge vs Pi" but "aforge's Pi-equivalent core vs
Pi" — and that core should be Pi-sized, with everything else paid for only when it is used.
**That is the whole strategy in one line: a Pi-sized core, pay-per-use for the rest.**

## 1. Where the bytes go, by kind

Page (23.4 KB): 34% essential law · 10% the same law said twice · 42% dead on most turns
(standing 2.5 KB, accounts 1.1 KB, landed-task family 1.8 KB, media essay 0.85 KB,
interrupt/woken/carry-on 1.6 KB, sub-harness prose 0.7 KB) · 14% style (aphorism essays,
shouting, metaphor).

Tool block (24 KB): 7.7 KB tool prose, 14.9 KB schema, of which 8.1 KB is *parameter*
prose; 62% of that sits on `propose_task` (14 fields) and `tasks` (10 optional,
mutually-exclusive verbs). The seven Pi tools grew 44%; four are byte-identical to Pi.

Per turn on top (a 6-round bug fix, one job out): ~10 KB dynamic, of which the job footer
is re-sent on every round (150 B × 21 = 3.2 KB per turn; three fork hands out ≈ 19 KB of
footer per turn). And the chat's tool-result cap is a flat 50 KB regardless of window —
one `read` is 78% of a 16k window. That is the biggest single number in the audit and it
is not a prompt problem.

Concrete duplicates the audit confirmed:

- the four-words landing law is on the page twice (static `system.md` and the `tasks` belt fact);
- "ask through `ask`, never prose" three times — and `ask`'s schema is not carried;
- "results arrive on their own, never poll" ten times across five tool descriptions, zero times as one law;
- the 853-byte media-prompting essay ships unconditionally on a belt where every media verb is shelved;
- the page explains `[carry on]` (536 B) and `[something you set up fired]` while both messages
  already carry their own instruction inline (`checkpointCarryOnLead`, `standingNewsRule`).

## 2. Delivery classes — every law gets exactly one

The anti-spaghetti move is not a cleverer renderer; it is a **taxonomy every byte must be
filed under**, with one mechanism per class, and a test that a law is filed once. Today
there are four conditional-text mechanisms (page-level predicates in `prompt.go`,
sentence-level predicates in `beltfacts.go`, the `load_capability` shelf, the tail
volatile note) and no rule for which one a given sentence belongs in. That is why the same
law ends up in three of them.

| Class | Delivered by | When it costs | What belongs there |
| --- | --- | --- | --- |
| **DELETE** | — | never | any law already stated elsewhere; any prose about a verb this belt does not carry |
| **CORE** | `system.md`, byte-stable for the session | every request | identity, tone, the routing table (which verb for which shape), workflow, delivery, critical. Target ≤ 6 KB |
| **WITH THE VERB** | the tool's own description | only while the verb is carried | the tool's *contract*: what it does, its fields, its limits, what comes back. Never "when to prefer it" — that is the routing table's one line |
| **WITH THE EVENT** | the harness message that announces the event | only on the turn it happens | how to read a landing note, a `[carry on]`, a fired standing item, a job exit. The message is self-describing; the page never explains a message |
| **ON DEMAND** | `manual`, and the `Loaded:` reply of `load_capability` | when the model pulls | long mechanics the model will not need until it is inside the thing: media prompting, standing's `when.in`/`when.at` grammar, account etiquette, harness design |

Two laws about the classes:

1. **Existence is CORE; mechanics ride with the verb; consequences ride with the event.**
   "Remind me at 6" needs the model to know `stand` exists *before* it plans — one line in
   the routing table. How to spell `when.at` is needed only at the call — the description
   already owns it (the budget ledger says so). What to do when one fires is needed only
   when one fires — the news line already owns it. The 2.5 KB standing section is the
   three of those stacked on the page; only the first line belongs there.
2. **Verbs can be pulled; laws must be pushed.** A model asks for a tool it can see the
   name of (`load_capability`'s catalog). It never asks "is there a law about X". So the
   shelf is right for verbs and wrong for laws, and ON DEMAND law needs a recognisable
   trigger in CORE: "anything about aforge itself → `manual`" works because a question
   about aforge is recognisable. "Making media? read the manual page" is the same shape.

## 3. Ideas considered, and the critique of each

**Delete the duplicates.** ~2.1 KB page + ~6.7 KB tools, no law leaves. The one general
sentence "anything handed off — a job, a watch, a task, a quick task — reports itself into
this conversation; never sleep, tail or poll" replaces ten. Risk: none that a test would
not catch; the pinned phrases are listed in §6. Verdict: do first, unconditionally.

**Turn-triggered sections in the page** (inject standing law when a time word appears,
accounts law when one is connected). Critique: it busts the cached prefix on the exact
turn it fires, trigger detection is heuristic, and a missed trigger silently removes a
law. Verdict: **reject in message[0]**. The same content delivered WITH THE EVENT or ON
DEMAND is free of all three problems.

**Self-describing harness messages.** Already the pattern for `[carry on]` and standing
news; extend to the landing note ("this landed `your call`; say that word back; answer the
request it was for, do not restate that it finished") and the job exit note. The page's
interrupt/woken/carry-on/four-words paragraphs (~2.3 KB) become DELETE. Critique: a
message that carries instruction reads as the person to a small model — `volatileNoteOpening`
exists for that reason; keep the framing sentence, keep it short. Verdict: adopt.

**Prose on the shelf with the verb.** `capabilityGroup` gains a `prose` field emitted in
the `Loaded:` reply; the page stops teaching media, harnesses, settings and `ask` (~2.5 KB
today, all for verbs the model cannot currently call). Critique: the routing table must
still name the group so the model knows to load it — one line each. Verdict: adopt; it is
the existing shelf with one field.

**Let a reflex route law sections per message** (the memory router already runs twice a
turn and picks lines). Critique: adds a model call, latency and nondeterminism to a
correctness-critical decision; a routing miss removes a law with no trace. Fine for fuzzy
retrieval (memory, manual); wrong for law. Verdict: reject for law.

**Learned pruning** (drop a section that a model never seems to use). Verdict: reject;
silent behaviour drift is the failure the changelog rules exist to prevent.

**Hierarchical page: a table of contents that points at manual pages.** This is the middle
path for rare-but-planning-relevant law: CORE keeps one line of existence plus pointer,
the manual page holds the mechanics. Critique: it costs a `manual` round-trip the first
time, and a small model may not follow the pointer. Verdict: adopt for standing, accounts,
media, harness design; measure whether the first-call cost shows up in the bench.

**Tiered descriptions per model class** (a `terse` string beside each description).
Critique: two wordings of one contract drift, and the law tests would need to pin both.
Verdict: prefer **one terse wording for everyone** where the terse one loses no contract
(most parameter prose), and reserve the two-tier form for the two or three descriptions
where the long form measurably helps a frontier model (`propose_task.brief` is the
candidate; prove it on the bench before keeping two).

**Enums over mutually-exclusive booleans on `tasks`.** Ten optional fields with prose
exclusivity is the single schema most likely to be mis-called by a small model
(`{"id":"7","stop":true,"say":"please stop"}` is invited by its own text). An `action`
enum with `required` is what JSON-schema tool training actually teaches. Critique: it is a
wire change to a verb the page, the manual and several tests spell; and frontier models
handle the present shape. Verdict: a wave of its own, after the diet, with the lean profile
as its first customer.

**A lean profile keyed off what we already know.** No new dial: the model's window
(`ContextWindowFor`) and the crew's open-weight `worker` tier are the two facts. Under a
threshold: CORE only, the seven Pi tools plus `quick_task`, `questions` pre-armed (no
load-then-ask two-step, which a one-call-per-message model cannot do), `propose_task`/
`tasks`/`watch`/`track`/`commit`/`recall`/`read_document` on the shelf, result caps scaled
via `ctxbudget`, memory reflex off, instruction file at 2 KiB. ≈ 9.5 KB, Pi parity.
Critique: a profile is a second product to test; without a bench cell it will rot. Verdict:
adopt, and it exists only if `prefixbudget_test` weighs it and a local-model bench cell
runs it.

**Ablation as the arbiter.** The discipline paragraph earned its place with twelve
unattended runs (3/3 vs 0/5). Nothing else on the page has that evidence. For the ten
largest law units, run the same rig with the unit removed; keep what moves the outcome,
file the rest as ON DEMAND. Critique: expensive (Spark, a day of runs). Verdict: do it for
the top ten only, and never cut a pinned law on bytes alone.

## 4. The tool block — contract, not policy

- **Description = contract.** `bash` today carries routing policy ("work whose outcome is
  a deliverable belongs to `propose_task`"); that is the routing table's line, not the
  tool's. Every "reach for this when" sentence moves to the table, stated once.
- **Parameter prose is one clause.** A schema law test (the `iconlaw` shape): no
  ALL-CAPS runs, no en/em dashes, description ≤ ~160 chars unless it is a judge paragraph
  registered by name. `propose_task` 5,720 → ~2,900 with no field losing its rule.
- **The Pi seven stay Pi-shaped.** `edit`/`grep`/`find`/`ls` are byte-identical; keep them
  so. `read`/`bash`/`write` keep their genuine contract additions (append, salvage,
  background, described media) in half the words.
- **`quick_task` is the template**: the judge once, in the verb; the belt bullet names the
  verb only; the page names it once in the table. New verbs land in that shape or not at all.

Expected: 24.0 KB → ~17.4 KB with every law still stated somewhere; ~12.5 KB with the
lean shelf; ~7.8 KB for the lean profile.

## 5. The dynamic bill — fix these regardless of any profile

These are defects, not style, and they cost more on a small window than the whole page:

1. **Scale the chat's tool-result caps to the window.** `tools.go:119` calls
   `bare.AllTools(workspace)` and discards the budget that `internal/exec/tools.go`'s
   `toolBudgetsFor` already derives from `ctxbudget`. Two call sites (`tools.go`,
   `tools_pdf.go`). Descriptions quote the numbers, so it is manual law too; tests pin the
   literals in both directions and need fixtures sized off the budget.
2. **The job footer defeats de-duplication.** `reportedJobs` is set on the whole result, so
   while any job is out no result in that leaf collapses to the 120-byte pointer. Hash the
   stripped body; keep the field.
3. **Strip the footer in `toolcompact.go` and `stub.go`.** Today up to 300 of the 400 bytes
   of a compacted tail are footer. One-line change, no wording moves.
4. **Cap and cache the decisions record.** It is unbounded, lives in message[0], and is
   re-read on every refresh — the one thing left that re-prices the prefix as it grows.
5. **Footer only when it changed**, and every footer budgeted inside its own cap the way
   `boundedResult` already is. The page's "hands still out ride at the foot of every
   result" promise moves with it.

## 6. Guardrails, so the diet cannot regress

- **A law registry test.** Each law unit has an id, a class and a key sentence; a
  structural test fails if a key sentence occurs more than once across page + tool block,
  or if a WITH-THE-VERB sentence names a tool the belt does not carry. This is the manual
  law's shape applied to duplication: make the defect a build failure.
- **Budget per profile**, and the cap comes back down. Lane K raised 48,000 → 49,000 with
  the debt ledgered; the diet's target is ≤ 40,000 for full and ≤ 10,000 for lean, and the
  number only ratchets down.
- **Wording coverage for `beltfacts.go`.** Only `standingFacts` and four floor-node
  fragments have their wording asserted; every shelved wording and the whole of
  `handoffFacts`/`programFacts`/`revisionFacts` is unguarded. That is the surface the
  rewrite touches; the tests land with it.
- **Pinned laws that do not move on bytes alone**: the working discipline
  (`taskprompt_test.go`, byte-identical in chat and worker), "depth of checking"
  (`proportion_test.go`, no digits allowed), "one breath", the asked-again and dowry
  passages (`task_escalation_test.go`, anti-narrowing), "no planner on your belt" and
  `wide` (`task_divide_test.go`), the re-execution and numbers-from-conversation lines
  (`prompt_reexecution_test.go`), `bash`'s "finishes or its armed bound"
  (`tools_jobs_test.go`). Cut around them or bring evidence.
- **The checkpoint sidecar asks** were tuned off-policy on `bench/oneroad/replay`; they are
  not on the per-turn bill and do not move without rerunning that bench.
- **Proof is the bench, not the byte count.** Before/after on the TUI e2e suite and the
  benchmark cells with a frontier model (must not drop), plus one local-model cell
  (deepseek flash or a Qwen-class model) for the lean profile. Run on Spark.

## 7. Sequence

1. **Defects first** (§5 items 2–4, then 1): no wording moves, measurable on any window.
2. **DELETE pass** (§1's duplicates; the never-poll law once; the media essay off the page;
   `tasks`' second four-words copy), landing the law-registry test with it. Wait for
   quick/choice to merge first — it owns the handoff section.
3. **WITH THE EVENT**: self-describing landing and job-exit notes; the page's
   interrupt/woken/carry-on paragraphs go.
4. **ON DEMAND**: shelf prose in the `Loaded:` reply; standing/accounts/media/harness
   sections become one routing-table line plus a manual pointer each.
5. **Tool block**: contract-only descriptions, one-clause parameters, the schema law test.
6. **Lean profile**: keyed off window + worker tier, its own budget line, its own bench cell.
7. **Ablation of the top ten**, then the `tasks` enum wave.

What not to do: raise the cap; inject anything turn-triggered into message[0]; cut a pinned
law on bytes; cut the project's own instruction file for frontier models (it is the
highest-value text in the prefix); ship the lean profile without a bench cell.
