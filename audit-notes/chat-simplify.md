# Chat simplification — from case law to one loop over one memory

Date: 2026-08-12, revised three times same day after product review. Branch
`chat-v2`. Companion to `chat-rebuild.md` (the teardown that built the current
head) and `chat-call-graph.md` (the trace of what runs today). The current
head works, but it is a compliance officer wearing 9,000 tokens of law, 28
lexical vocabularies, and 29 tools. The goal is the opposite shape: **a small
agent loop with general judgment, standing on mechanism that makes the
dangerous mistakes impossible** — so the prompt can stop enumerating them.

---

## Part 0 — What the product is, and what that decides

aforge is a resident employee. One conversation is the whole interface: you
say things there, work happens behind it, results come back there, and
everything you own — running work, standing rules, services, learned ways of
working, what it knows about you — is reachable from that same conversation.
The journal is the truth; learning loops tune behaviour over time; nothing is
hardcoded that could be learned (the emergent-capability principle).

Six architectural consequences, and every design choice below follows from
them:

1. **Chat is intent capture and reporting — never execution shape.** The person speaks in outcomes ("make 10 reports instead of 2", "don't do this"). The chat's job is to bind that intent to the right object and the right verb, and *nothing else*. What the intent means for a plan is the tasker's interpretation problem.
2. **There is exactly one seam between chat and work: the command funnel.** Chat never talks to a planner, a sentinel, a worker or a craft directly. It journals a command; the tasker side interprets it. This seam already exists (`RequestCommand` → reconciler) and is the one piece of the current architecture that is exactly right.
3. **The person never needs a verb vocabulary.** They say what they want in their own words. The model needs a *small* verb vocabulary — and the smaller it is, the less it can fumble.
4. **Nothing the system knows is unreachable from chat.** The journal is the truth and the conversation is a lens over it (Decision 7) — so the lens must be total: live work, a running step's own progress, any finished result, any file, anything ever said or learned, and the system's own health and spend. If the store knows it, chat can surface it.
5. **The reply is written for the reader, sized to its content.** Raw is for the loop's own reading; composed is for the person. "Task done" is never an answer; a reply cut at an arbitrary token ceiling is never an answer either. The last mile — from settled work to spoken answer — is a real turn with real reads, not a relay.
6. **Defaults are learned, not legislated.** Where the model faces a genuine judgment call (do this here vs. hand it over), the answer is: judgment first, a cheap structured question when genuinely unsure, and a learning loop that turns repeated answers into defaults — using machinery that already exists.

Standing assumption (confirm if wrong): the chat loop stays seconds-scale.
"Multistep" means reads, recall, small acts and asks — not minutes of compute.
Anything needing a workspace and time is the tasker's, always.

---

## Part 1 — Where the complication actually is (measured)

### 1.1 The prompt is case law

~~14,000 chars system prompt + ~22,000 chars of tool descriptions = **~~9,000
tokens fixed, every message**. Inside it:

- **50 "never", 1 "must".** The law section (5,008 chars) is 3× the job description (1,734 chars).
- Every law is a memorial to one incident (bd3c78ed's SVG, the double stock job, "I've put it in hand"). Real failures — but the median message is "open the pdf", and it pays the full catastrophe tax.
- The doctrine is stated twice: once in `orchestratorLaw`, again inside the 29 tool descriptions (spawn's alone is ~250 words of policy). The old router-vs-belt split brain reborn as prompt-vs-descriptions.

### 1.2 Six laws in the prompt are already enforced in code

| Prompt law | Code that already guarantees it |
|---|---|
| never claim work started without a receipt | `run.summary()` builds from receipts only |
| never promise an unkeepable route | `keepable()` strips it post-hoc |
| a follow-up to live work is a change, not a job | `amendmentFor()` door |
| irreversible things never ride a reflex | `consequenceGated()` at the journal door |
| never commission the same thing twice | `alreadyCommissioned` + `pendingTwin` |
| needs_confirmation = nothing changed | confirm gate ends the turn structurally |

The model spends attention complying with rules it cannot break.

### 1.3 The lexical empire

**28 keyword vocabularies** across the head package (`correctiveOpeners`,
`controlVerbs`, `classVocabulary`, `urgencyPhrases`, `selfQuestionPhrases`,
`reportBackCues`, `handCues`, `dispatchCues`, `charterVerbs`, stop-word
maps…). Each written after a live failure, each answering it by learning more
words. They feed `hints.go` (245 lines of recognizers the law then tells the
model to distrust), `amend.go` (a prefix list deciding new-job-vs-revision),
`class.go` (621 lines of NL set-surgery parsing), `promise.go` (cue lists
scanning the reply for forbidden promises), `urgency.go`, and the cue halves
of `control.go` and `charters.go`.

### 1.4 The tool sprawl

29 tools; 11 are reads that are all "search or fetch something from the
journal", sitting on **5 separate FTS indexes** (`messages_fts`, `graph_fts`,
`facts_fts`, `charters_fts`, `services_fts`) with 7 separate `Search*`
functions. Three doors to "make work" (`spawn`, `fork`, `correct`), five to
"change work" (`control`, `steer`, `revise`, `expedite`, `craft`) — a verb
taxonomy the model must learn and the person never sees.

The steer/revise split is the clearest tell that the taxonomy is
machinery-shaped, not person-shaped: "use latex please" should *both* tell
running workers *and* fix the plan — and in fact the redirect path already
does both (`redirect.go:120` broadcasts, then the revision runs). The person
has one intent; the belt makes the model pick between two halves of it.

### 1.5 The one-shot turn is the root cause

The turn cannot pause, report, and continue; it cannot observe the outcome of
a command it issued (`await` = 3s for a receipt). This single constraint
forces the 8-call belt and 6-read cap with mid-turn budget nagging, the
DIAGNOSIS IS NOT DISPATCH law, receipt-voice written before the compile has
decided the job's shape, and `promise.go` policing the future tense. Fix the
turn model and four laws, two caps, one tool and one cue file become
deletable.

### 1.6 Truncation-by-design

The same fear that wrote the law also set the ceilings: replies capped at
1,200 tokens (`orchestratorMaxTokens`), the delivery sentence at 600
(`absorbMaxTokens`) over a 4KB read of the result (`absorbResultBytes`),
one job's result at 4KB (`beltResultBytes`), self-reads at 2KB. The original
incident (a deliverable authored inline and cut in half) was fixed by the
write tool and the truncation mark — but the ceilings stayed, and now they
cut *answers*: the head literally cannot read a whole report before speaking
about it, and cannot write a long-form answer even when the content deserves
one. The delivery turn is toolless besides — forbidden to restate the result
because the raw result is pasted verbatim underneath, which is the machine
showing its output instead of answering the person.

### 1.7 Module boundaries that lie

`internal/head` (30,331 non-test lines) contains the **tasker's compiler**
(632 lines). `cmd/aforge/chat.go` (4,453 lines) owns delivery judging, plan
revision wiring, the consent desk, the provider pool — engine logic
unreachable from `internal/`.

---

## Part 2 — Target shape

### 2.1 Six components — responsibilities, contracts, interactions

```
                    ┌────────────────────────────────────────────┐
                    │                  journal                    │
                    │    (one SQLite store, one command funnel)   │
                    └────────────────────────────────────────────┘
                      ▲ read        ▲ read/write     ▲ write
                      │             │                │
   ┌──────┐  reads  ┌─┴────┐   ┌────┴────┐  admits ┌─┴──────┐
   │voice │────────▶│ lens │   │  gates  │◀────────│ tasker │
   │(chat │         └──────┘   └────┬────┘         │        │
   │ loop)│  commands (funnel) ─────┼─────────────▶│compile │
   │      │◀──── wakes: receipt / delivery ────────│plan    │
   └──┬───┘                         │              │run     │
      │                             ▼              │revise  │
      ├── hands (act, write)   questions to        │craft   │
      └── keep  (note, forget)   the person        └────────┘
```

**voice** — the chat loop. *Owns:* folding/refold, the prompt, the tool loop,
interim speech, asks, and the composed answer for every receipt and delivery.
*Never does:* compile, plan, decompose, interpret a change, touch legality or
money. *Talks to:* lens (reads), the funnel (commands), hands, keep, gates
(only as the recipient of questions to relay).

**lens** — the entire read surface, total over the journal and live state.
*Owns:* the four reads (2.2), the one BM25 blend over the five FTS indexes,
every renderer (the TUI's task pages and the head's board come from the same
queries — one-renderer law). *Never does:* writes of any kind.

**tasker** — everything between a command and a delivery. *Owns:* compile,
craft recognition, plan, JIT division, run, the revision judge, admission
control (dedupe/twin — moved here from the head, 2.6), progress narration
duty (2.5). *Never does:* conversation; its outputs are journal rows.

**gates** — consent desk, daily rail, confirm questions, question identity,
ask-or-assume learning (`ShouldAsk`). *Owns:* every question the system puts
to the person and every deterministic answer path.

**hands** — `act` (instant, reversible, seconds) and `write` (artifact to
disk). *Owns:* the consequence gate on `act`.

**keep** — the notebook: durable beliefs, preferences, corrections; retrieval
into the prompt; supersede/quarantine.

Interactions are exactly five, all through the journal except hands:

| From → To | Carrier | Content |
|---|---|---|
| voice → tasker | command funnel | `Task`, `Change`, `Stop` |
| tasker → voice | wake (receipt / delivery rows) | structured Receipt, Delivery (2.5) |
| voice → lens | function call | the four reads |
| gates → person | question rows | consent/confirm/scope questions; answers return deterministically |
| voice → hands/keep | function call | instant act, file write, note |

Nothing else talks to anything else. A seventh interaction anywhere is a bug.

### 2.2 The read surface: four reads, total observability

The product rule (Part 0.4): if the store knows it, chat can surface it. Four
reads cover the whole space by tense and grain — present, past, one-thing,
self:

| Read | Tense/grain | Serves |
|---|---|---|
| `board(q?, status?)` | present, many | live work: every running/queued job, one line each |
| `recall(q, kind?, since?, until?, session?)` | past, many | everything settled or said: messages, results, plans, beliefs, rules, services, files — one BM25 blend, typed filters, real content snippets with ids to open |
| `open(id, part?, raw?)` | any, one | one thing whole, state-aware: a **running** job → its plan with per-step status, live progress feed tail, files so far, spend so far; a finished job → result, files, spend, how parts ended; a file → its bytes; a rule/service/craft → its full record. `raw:true` returns the underlying journal rows/trace unredacted |
| `status()` | present, self | the whole system in one page: running/queued counts, today's spend vs rail, standing watches and next checks, services, learned ways, measured competence, residency/health heartbeats |

This dissolves today's eleven reads into four, and adds the two things
genuinely missing today: **live detail of a running node** (nothing serves a
running step's own progress to the head — only the TUI sees it) and **one
system-status page** (today split across three tools and a TUI pane).

**Reads are paged, never silently truncated.** The 4KB-league byte budgets
die. A read the loop chose is worth its bytes: `open` returns the whole
thing, or — past a sane transport page — a first page plus an explicit "N
more pages via open(id, part:2)". The loop always knows when it is holding a
fragment, and the person can always get the rest. This is the *retrieval*
half of the raw policy: **raw in, composed out** (2.6).

### 2.3 The work verbs: create, change, stop — and nothing else

The person's intents about work reduce to three shapes, so the model's tools
do too:

| Tool | What it means | What dies into it |
|---|---|---|
| `task(ask, context?, amends?, after?, fresh?, model?)` | one piece of new work, written richly | spawn, fork (context travels), correct (amends names a settled job), craft-run (naming the way is the override), model-words (model arg) |
| `change(target, words)` | "this thing, but different" — the words are the person's, verbatim | steer, revise, expedite, reprioritize, rule-cadence/wording, service-restart flavors, craft-revert |
| `stop(targets, words?)` | withdraw something | cancel, pause, rule-retire/hold, service-stop, craft-retire |

The critical move is **`change` carries no verb**. The chat binds the intent
to a target and journals the person's words; the tasker's revision judge —
which already exists (`reviseForUser`, `RevisionFlavor`, `JudgeRemainder`) —
decides what the words mean: a plan edit ("make 10 reports instead of 2" →
remaining plan re-planned), a broadcast-only constraint ("be careful with the
tone" → no plan change, running leaves told), an urgency read ("hurry" →
expedite flavor), or both. The broadcast-and-edit pairing is already how the
redirect path behaves; the merge codifies what the machinery does.

Targets are anything the person owns — a job (live or settled), a standing
rule, a service, a learned way of working. The store's legality table and the
consent gates already discriminate per kind; that is where the safety lives
today and it does not move.

So the product questions have one-line answers:

- *"don't do this"* → `stop(job)`; consent gate if expensive.
- *"make 10 reports instead of 2"* → `change(job, words)`; the revision judge replans the remainder and tells the running workers.
- *"broadcast? planner? sentinel?"* → never chat's question. Chat binds intent to target; the funnel is the only seam; planner and sentinel are internal organs of the tasker with no chat-facing names.

### 2.4 The full toolbelt: 29 → 13, descriptions as interfaces

`board`, `recall`, `open`, `status` · `task`, `change`, `stop` · `act`,
`write` · `note`, `forget` · `ask` · `interrupt`.

Every description one sentence plus arg docs; all surviving when-to-use
policy lives in one short prompt section. Measured target: tool definitions
**~5,500 → ~1,300 tokens**. (`answer_question` folds into gates; `await` dies
into the receipt wake, 2.8A.)

### 2.5 What the tasker owes the chat — three contracts

The seam stays one funnel wide, but what flows back gets typed. Today the
receipt is prose in a system voice ("using your person-research-dossier way
of doing this — 3 steps, ~$2.50 cap") that leaks machine vocabulary straight
into the room. Three contracts fix the direction of translation — the tasker
reports structure, the voice writes the sentence:

1. **Receipt** — settles every command. Structured: what was understood (the reading), the shape chosen (steps, craft used or not), the estimate and cap, what a change actually did to the plan (for `change`), what was withdrawn (for `stop`). The voice's receipt-wake turn (2.8A) renders it in its own words; the raw row remains for `open(raw:true)`.
2. **Progress** — a duty, not an event: every job root owes the journal periodic plain-language progress lines (largely exists as the task-room feed today; the contract makes it required and person-readable, because `open` on a live job serves it to the head).
3. **Delivery** — a finished job: summary, files with paths, spend, how parts ended, and *what fell short* if anything did. The delivery turn that consumes it is specced in 2.6.

Two things move *into* the tasker from the head, because every surface should
benefit and chat should carry no work-shaping logic:

- **Admission control** — `pendingTwin` / duplicate-ask dedupe runs at the funnel door, so a headless `aforge do` gets the same protection chat gets.
- **The whole interpretation of change** — the revision judge formally owns steer-vs-replan-vs-expedite; chat's `change` is words + target, nothing else.

### 2.6 The answer contract — the last mile

The raw question has two sides, and they get opposite answers:

**Retrieval — raw in.** The loop reads actual bytes: results whole, files
whole, journal rows when asked. Paged, never silently clipped (2.2). The
model cannot compose a good answer over a 4KB peephole, so the peepholes go.

**Response — composed out.** Everything the person sees is written for them,
by the voice, sized to its content. The raw output of work is a record the
lens can always serve — it is not the message.

What that means concretely:

- **The delivery turn becomes a real turn.** Today's absorb is toolless, capped at 600 tokens over a 4KB peek, and *forbidden* to restate the result because the raw result is pasted verbatim underneath. Inverted: when work settles, the head is woken with the structured Delivery **and its lens** — it opens the result, reads the file if that is where the substance lives, and answers the person's original ask: the findings, the verdict, the numbers — then the pointer: "full report at ~/reports/q3.pdf". The verbatim result row stays journaled as the record (the task room renders it; `open` serves it), but the room's *answer* is the head's composed reply. "It's done" is never an answer; neither is a pasted log with one sentence on top.
- **A file the head wrote itself needs no re-read** — it authored the bytes this turn; it answers with the substance and the path in the same breath.
- **Trivial things are done, not dispatched and not narrated.** "Open the pdf" is `act` and one line back. The boundary (2.7) keeps trivial asks out of the tasker; this contract keeps the *tone* right — the interface behaves like a person doing a small favor, not a system logging a dispatch.
- **The reply caps die.** `orchestratorMaxTokens` (1,200) and `absorbMaxTokens` (600) were truncation-by-design and they cut real answers. What replaces them is judgment plus the guards that actually worked: the artifact law as a principle (document-shaped things go through `write`), the truncation *mark* mechanism for provider-side cuts (12.5 — any cut is visibly a cut), and a generous runaway bound in the loop (calls, not words). A reply as long as its content is correct; a reply cut mid-SVG was the defect the law was written about — keeping the cap keeps the defect, self-inflicted.

### 2.7 The delegate boundary: floor → judgment → ask → learn

The one place the model must be very clear: real work goes to the tasker.
Four layers, none of them a keyword list:

1. **Floor (code).** Inline hands are structurally incapable of work: `act` is one command, seconds, reversible, consequence-gated — and its refusal names `task` as the route (all already true). Nothing in the loop can produce a deliverable except `write`, and nothing can take minutes.
2. **Judgment (prompt, ~4 sentences).** *"You do two things yourself: find things out, and instant reversible acts. Anything that produces a deliverable, takes real time, or can't be undone is work — hand it to the workforce as ONE task, written richly: their words, the context that settles what 'done' looks like, the constraints you both know. You never split work into pieces; the workforce decomposes. When you genuinely can't tell whether they want the quick look now or the proper job, ask."*
3. **Ask (mechanism, cheap).** The existing `ask` tool: one numbered question, options as clickable rows, turn ends, answer returns through the deterministic gate. Phrased in outcomes, never machinery: not "should I make this a task?" but "Quick answer from what I already have, or a proper researched report?"
4. **Learn (existing machinery, new category).** `ShouldAsk` (`store/meta.go:561`) already implements VOI-learned ask-or-assume per question category with consent-bearing exceptions; `RecordAssumedWithDefault` already journals skipped asks so corrections match back. Register `scope` as a category: early boundary asks are real questions; consistent answers collapse into a declared default; durable per-person patterns get a `note`. Zero new learning architecture.

### 2.8 The three mechanism changes that buy the prompt cuts

**A. Receipt/settle wake — the head hears back.** The absorb path already
wakes the head when a delivery row lands. Generalize: a command the head
journaled wakes it when the receipt settles, for a short bounded follow-up
turn over the structured Receipt (2.5). Consequences: `await` dies; the
commit-before-shape defect heals (the head says "handing this over", then one
wake later says what the tasker actually made of it, in its own voice);
`promise.go` shrinks to nothing because "I'll tell you what it decides" is
now keepable; `change` becomes honest because the head can report what the
revision judge actually did.

**B. Interim speech — the loop may talk mid-turn.** A `say` that does not end
the turn. Consequences: the read/act split cap dies; one generous runaway
bound remains (calls, not words); the spent-belt nag strings die; "have a
quick look and tell me" becomes one honest turn.

**C. The lens-armed delivery turn** (2.6) — the delivery wake carries tools
and no token ceiling, so the answer to settled work is composed from the
actual deliverable, not relayed past it.

All additive; none touches the store schema.

### 2.9 The prompt: ~3,500 → ~1,200 tokens, principles not statutes

Five short sections:

1. **Who you are** — a person with a job, not "the orchestrator of a task-graph agent". The identity carries the voice.
2. **What you have** — the four reads, the three work verbs, hands, the notebook. Six sentences.
3. **Judgment** — the boundary paragraph (2.7.2) plus: *Ground every claim in something a tool showed you this turn.*
4. *Deliverables are files; conversation is meaning.*
5. *A follow-up about work in flight changes that work; a genuinely new ask is new work; when several things match, ask.*
6. *Numbers are quoted, never derived.*
7. *An honest miss beats a fluent reconstruction.*
8. **Gates** — one paragraph: a gate may stop a change and ask; the question is the reply; consent questions belong to the person.
9. **Voice** — a handful of positive lines: the first sentence answers; size the answer to its content — substance first, pointer after; when work settles, answer the ask it was commissioned for, never report completion; speak in their vocabulary (one sentence; `open(raw:true)` on explicit ask is the sanctioned exception).

Deleted outright, replaced by mechanism: the HONESTY claims
(summary/keepable), DIAGNOSIS IS NOT DISPATCH (dies with the turn model),
CONSENT mechanics (gates), the hints paragraph (hints die), repair-doctrine
deliverability checking (natural once turns continue), the artifact law's
threat clause (the cap that made it a threat is gone; the principle stays).

### 2.10 What the lexical empire becomes

| Today | Fate |
|---|---|
| `hints.go` + ten recognizers | **delete** — the loop reads the board itself |
| `amend.go` prefix trigger | **delete trigger**; amendment = `task(amends:)` judgment + prompt principle; `pendingTwin`/`alreadyCommissioned` **move to the funnel** and stay |
| `class.go` NL set-surgery parsing | **delete** — the model names ids from the board; unit rule + consent gate downstream unchanged |
| `promise.go` cue lists | **delete** after 2.8 lands |
| `urgency.go` | **delete** — urgency is a `change` the judge reads |
| `control.go`/`charters.go` cue halves | **delete** |
| `modelwords.go` | keep the feature, move it: `task(model:)`, resolved tasker-side |
| steer/revise/expedite split | **merge** behind the revision judge |
| reply/absorb/read caps | **delete** (2.6); truncation marks and paging stay |
| answer-side gates (worker question, numbered choice, rail "yes") | **keep exactly** — deterministic consent answers are non-negotiable |
| `consequenceGated`, dedupe, fold/refold, question identity | **keep** — mechanism earning its keep |

Net: head package lands around **half its 30k lines**, with `compiler.go`
moving out besides.

---

## Part 3 — Migration order (each step ships alone)

1. **Prompt + tool-description rewrite.** No mechanism change. Delete the six code-enforced laws, collapse descriptions to interfaces, rewrite desk and voice, add the boundary paragraph. Guard with the golden-turn/journey suites. Removes ~7k fixed tokens.
2. **The lens: `recall` + `open` + `status`, paged reads.** Additive store views over the five FTS indexes and the live feeds; old read tools stay wired until parity, then drop out of the belt. Delivers Part 0.4 (total observability, including live node detail) and the retrieval half of the raw policy.
3. **Receipt wake + interim speech + the answer contract.** The delivery turn gets the lens and loses its caps; reply caps die; truncation marks stay. Then delete `await`, the read cap, the spent strings, `promise.go`. Type the Receipt (2.5.1) in the same wave — the wake turn is its consumer.
4. **The verb triad.** `task`/`change`/`stop` land; `orders`/`fanOutLimit` die; amendment door becomes `amends:` + judgment; dedupe moves to the funnel; steer/revise/expedite merge behind the revision judge.
5. **`scope` question category** wired into `ShouldAsk` + `RecordAssumedWithDefault`; boundary asks start learning.
6. **Lexical deletions** (hints, class NL, urgency, cue halves) once 4–5 are proven against the journey suite.
7. **Module extraction** — compiler out of `internal/head`, engine logic out of `cmd/aforge/chat.go`, packages named voice/lens/tasker/gates.

Steps 1–2 are low-risk and reversible; 3 is the payoff; 4–6 ride on 3; 7 is
hygiene that gets cheaper after the surface shrinks.

---

## Part 4 — Decisions taken, and what's still open

**Decided (this revision):**

- **Raw in, composed out.** Retrieval is raw and paged — the loop reads whole results, whole files, journal rows on request, never a silent 4KB clip. Responses are always composed for the reader; raw output reachable via `open(raw:true)` on explicit ask.
- **The delivery turn is a real turn** — lens-armed, uncapped, answering the original ask from the actual deliverable, pointer to files after substance.
- **Reply token ceilings are removed**; the guards that stay are the write tool, the truncation mark, and a call-count runaway bound.

**Still open:**

1. **`change` with no verb at all** is the boldest cut. Fallback if the revision judge misreads too often: `change(target, words, hint?)` with hint ∈ {constraint, replan, sooner} — still one tool, hint is evidence.
2. **Does `stop` fold into `change`?** Kept separate: withdrawal is consent-gated and definite — a destructive intent deserves an unambiguous door.
3. **Does the chat loop ever get minutes-scale hands?** Assumed no. If yes, a third lane (a "session job" the loop owns) is a different design.
4. **JOURNEY.md** is product law (one mouth, 19 journeys). Steps 1–6 should be provable against the journey suite as-is; where a journey encodes old tool names, the suite moves with step 4, not before.

---

## Part 5 — The product layer: chats (added after product review, same day)

The product has exactly two places to talk: the chat, and a task's page. The
missing use case is the WORKING CONVERSATION — alive for days or weeks,
drifting as it evolves, commissioning several tasks along the way, receiving
their results back into the conversation, resumable after weeks. Possibly two
or three alive at once. The engine already has the primitive (sessions: per-
room head cursors, delivery routing to the owning room, scribe naming, the
thread tool); the product never admitted it. The admission is called
**chats** — plural of the word the person already uses.

### 5.1 The law

1. Threads talk to the colleague; task pages talk to the work. Two talking surfaces, one became plural.
2. Creation and splitting are conversational offers, never chrome. The only deliberate gesture is typing.
3. One ornament: the unseen-delivery dot. No badges, counts, or competing color.
4. Naming is the scribe's job, silently; "call this thread X" works as a change.
5. The list is a switcher, not a manager: open, filter, enter. Nothing to organize, tag, or archive. Threads never close; they go quiet and sink.

### 5.2 The journeys (J1–J7, join the journey suite)

- **J1 split-on-divergence**: mid-conversation pivot → head offers "own thread?" as a numbered ask (category-learned like scope; consistent yeses collapse into acting with one clause said). Transcript breathes one ruled line + title chip; you are already talking.
- **J2 working session**: ideate → task commissioned FROM the thread → keep talking while it runs → delivery returns INTO the thread → more tasks. The thread is the desk; 1 thread : N tasks.
- **J3 switching**: one key (`t`) or click the title chip → quiet overlay list: name, "left at:" line, relative time, ● only where something landed unseen. Typeahead, enter, esc.
- **J4 re-entry brief**: first turn after a gap opens with the head speaking the arc — what this thread is about, positions taken, tasks run and what they returned, what was left open. Journal-derived, rebuildable.
- **J5 alive glance**: "what's going on" answers running work AND open threads in one breath; threads with unresolved arcs are alive.
- **J6 nudge**: parked-thread mention on the presence line / first morning exchange; the response teaches cadence (note/scope machinery).
- **J7 no lifecycle**: threads sink when quiet, recall reaches them forever; months later the head finds the old thread and offers it before starting fresh.

### 5.3 UI treatment (tui2, existing machinery, Apple-calm)

| Element | Treatment | Built on |
|---|---|---|
| title chip | thread name in the placeline, always visible; click = switcher | placeline + keychip |
| switcher | overlay list (J3), `t` / chip click, typeahead | overlay + palette idiom |
| birth/split | numbered ask rows | question affordance |
| thread break | one ruled line + title | blocks.Ruled |
| re-entry brief | the head's ordinary first message — content is the feature | nothing new |
| alive glance | open-threads section on the board home: name + left-at line | homes/board |
| delivery elsewhere | dot in switcher + one presence-line word | presence line |
| attribution | task page adds "for: "; activating it jumps to the thread | ran-by row |

### 5.4 The one cross-seam contract

The split answer must move the SURFACE to a new session, and the coupling
stays journal-only: the deterministic answer gate settles the split by
posting a system row in the old thread ("continuing in ") carrying a
typed message part `room-switch` whose payload is the new session id; the
surface applies it on its poll (switch composer + view), the head simply
serves the new room. No in-process channel, no second seam.

### 5.5 Engine cost

Three pieces, each roughly one agent-task: the re-entry brief (per-thread
journal projection injected into the first turn after a gap, plus arrival
reuse of deliverBrief), the split-offer judgment (prompt + ask category +
the room-switch settlement), alive/nudge surfacing (open-threads in the
board home, status read, presence line). The switcher/chip/ruled-line/dot
are the surface's half.

---

## Part 6 — The tasker, blown out (verified against chat-v2, 2026-08-13)

Eight parallel code audits (four on the tasker, four re-checks: chat-side
components, blocking points, context ceilings, headless entry points) re-read
the tree end to end. This part records what the tasker actually is, so Part 2's
six-component target has a measured "from" as well as a "to".

### 6.0 Status corrections to Parts 1–2 (things that moved since 2026-08-12)

- **The verb triad has landed** (`task.go`, `change.go`, `stop.go`) and **the
  lens has landed** (`lens.go`: `recall`/`open`/`status`, real paging) — but
  nothing was removed. The belt runs **25 tools**: both generations coexist
  (11 legacy reads still wired, `toolbelt.go:284-297`).
- `promise.go` and `amend.go` are **gone**; admission moved store-side
  (`store/admission.go:37`) — chat and `aforge do` share the twin/dedupe gate.
- `orchestratorMaxTokens`, `absorbMaxTokens`, `absorbResultBytes` are **dead**;
  the deliverable travels whole into the absorb turn. But the effective output
  ceiling is now `config.DefaultMaxTokens = 32768` applied to *every* call
  (`provider/client.go:197`), and the read-side 4KB-league caps all remain.
- `hints.go` is **fully alive** (12 recognizers) with `class.go` (621 lines),
  `urgency.go`, `control.go` cue halves — Part 2.10's deletions have not begun.
- The AGoT blueprint (`audit-notes/agot-implementation-blueprint.md`) is
  part-landed: W1 durable spec, W2 growth governor + `Satisfied` criterion,
  W5 claim-time JIT (resident only). W4 (harness at claim), W6 (joint
  objective), W8 (coverage/research), W9 (minimal edges) are not.

### 6.1 The whole machine — every loop, every seam, as built

```
 person ──types──▶ surface: tui2 (44k) ∥ tui1 (19k) — each with its OWN store
                   reads and renderers (3rd/4th account of the same rows)
                      │ 300–400ms poll                ▲ in-process stream chan
                      ▼                               │ (a 6th seam — violates
┌─────────────────────────────────────────────────────┴──── journal-only law) ─┐
│                        journal — one SQLite (WAL)                            │
│  events → nodes/edges/messages/commands/facts/charters/questions/usage       │
│  NO write funnel: 87 ad-hoc BEGIN IMMEDIATE sites, 100ms busy-sleep ladder   │
└──────────────────────────────────────────────────────────────────────────────┘
   ▲              ▲                    ▲                       ▲
   │ head         │ reconciler        │ runner                │ leaf workers
═══╪══════════════╪═══════════════════╪═══════════════════════╪════════════════
   VOICE (1 goroutine, 400ms poll — one turn at a time across ALL rooms)
   │ turn loop ≤16 calls, belt tools SERIAL (leaf loop parallelizes; head not)
   │ wakes INLINE in poll: absorb(delivery) 45s + receipt 45s — a row that is
   │   both can stall the person's next message 90s; claim maps are in-process
   │   (restart between land and wake ⇒ answer lost silently, forever)
   │ 18 RequestCommand sites; FIVE doors mint CommandSplice (task, correction,
   │   revision, charters ×2) — the "one door" doctrine is not yet true
   ▼
   ══ command funnel — CommandSplice / Redirect / … (the one intended seam) ══
   RECONCILER (1 goroutine, 500ms tick; r.mu held across the tick tail)
   │ per command, SERIAL: compile → craftCompile → plan.Build → titleSubtree
   │   (a cosmetic model call that GATES work admission) → store.Splice
   │ only target-less splices run concurrently (4); redirects/reflexes barrier
   │ + 23 clock-gated sub-passes ON THE SAME GOROUTINE, most under r.mu:
   │   standing-watch · services probes (serial HTTP) · overrun replans ·
   │   craft sweep · distill · consolidate · reflect · territory · practice ·
   │   taste · skills · learning digests   ← charter firing compiles+plans
   │   UNDER THE LOCK; AttachSession (chat open!) and AskQuestion wait on it
   ▼
   RUNNER (1 dispatch goroutine + 32 worker slots; ready = deps done, no waves)
   │ claim(CAS) → JIT: JudgeSplit(free) → ExpandOne(2-3 calls INLINE ON THE
   │   DISPATCH GOROUTINE — nothing else claims; a division ENDS the pass) →
   │   WorthKeeping → splice(children + gathering parent)
   ▼
   LEAF (one goroutine per slot)
   │ linear: turn loop, parallel tools, obs-decay @ ctx/2 capped 64KiB
   │ swe:    re-exec self (AFORGE_SWEPRO=1) — own engine, own router, own
   │         compaction ALREADY AT 60% of usable context (calc/overflow.go)
   │ then STILL ON THE SLOT, serial, ~10 round-trips after work is done:
   │   consent gate → retry-worker judge → sentinel (locks.pass HELD ACROSS
   │   the model call — siblings settle one at a time) → remainder judge →
   │   overrun replan → delivery gate (EMPTY REPLY = SILENT PASS) → gate
   │   revision (a full second leaf run) → regate → gap-extend → annotate
═══╪════════════════════════════════════════════════════════════════════════
   out-of-process:
   launchd 5min → aforge wake — ticks the reconciler, constructs NO RUNNER:
     a fired charter splices leaves that sit pending until some window opens
   aforge do — same brain (reconciler+runner+consent), headless — THE funnel
   aforge run/plan/revise/show — a SECOND ENGINE (exec.Scheduler, graph
     files, no store/gates/JIT/replan) — scheduled for deletion (Part 7.B)
```

### 6.2 Inside the tasker — compile · plan · run · revise

```
 command row (verbatim ask + provenance)
   │
   ▼ COMPILE   head/compiler.go (632 lines INSIDE the voice package — move)
   │  one 3,300-word prompt → goal · scale{lookup|task|project} · parts ·
   │  subharness hint · assumptions · builds_on   (no ai.WithSchema; freeform)
   │
   ├─ scale ≠ project ──────────▶ ONE leaf + contract (chat.go:3393)
   ├─ ≥2 independent parts ─────▶ bundle: side-by-side, NO planner call
   ├─ craft match ──────────────▶ learned subtree, bypasses plan.Contracts
   ▼ otherwise
   PLAN      plan.Build, BuildDepth=1 — depth deferred to claim time
   │   ground ∥ spine(N samples → medoid)      [terrain render: git, 2s, eager]
   │   → panel? (extra round; chat pins EnsembleNever — ~600 lines dead)
   │   → fanout (per stage ∥) → bind ∥ size → audit → expand loop
   │   → briefs ∥ → contracts (ONLY after the last brief — could pipeline)
   │   sizing picks subharness from a registry enum: known name ⇒ atomic,
   │   parts cleared; unknown ⇒ degrade to linear                (size.go)
   ▼   SubtreeFromPlan: drops container nodes, joins on "%s-n%d" strings,
   │   Spec opaque; plan doc journaled with NO projection (lost on rebuild)
   ▼ store.Splice (atomic admission; node row beats provenance)
   RUN       dependency-event dispatch; siblings parallel by construction
   │   governor: in-flight 64 ∥ AdmitLocal (NumCPU, load hysteresis —
   │   unreachable from the legacy path) ∥ daily rail ∥ job ceiling 90
   │   JIT at claim: free refusals (depth / taken-whole / <2 named parts)
   │   before any paid call — but children get NO bind pass (edge-less,
   │   inherit ALL parent inputs), NO brief, NO criterion (Done dropped)
   ▼
   REVISE    all flat, by design (the 27-round lesson)
      sentinel: add|remove|rewire|retitle only, checkingRule guard
      retry-worker: failed leaf re-enters ready on attempt+1, prior partial
        fed back as an EXTRA input (retry prompt strictly larger)
      overrun replan: judge-before-replan, ≤12 nodes, depth 0, no ensemble
      growth governor: rounds → node ceiling → daily rail → Satisfied
        (positive done-criterion, fails OPEN on error)
      delivery gate → gap extension → regate   (evidence: Ran 40×200B,
        notebook 1KiB; deliverable itself UNBOUNDED — asymmetry is fine,
        the fail-open parse is not)
```

### 6.3 What is right and stays

- **The funnel exists and both surfaces use it** — `RequestCommand` →
  reconciler is real; `aforge do` is chat's own brain headless, sharing
  admission, gates, judges, JIT, replan. Consolidation is deletion, not
  construction.
- **Claim-time JIT with free refusals** — the split question is asked when
  landed evidence exists, refused for free by code predicates first, accepted
  only past `WorthKeeping`. Cheaper than AGoT's paid per-node complexity call.
- **Dependency-event dispatch** — no wave barriers anywhere in scheduling.
- **Cache discipline is deliberate** where it exists: one shared plan render;
  stable-first ordering commented at every call site; contract tails; the
  leaf's byte-identical system prefix; deterministic wire encoding.
- **`internal/exec/context.go`** — hysteresis decay to a *measured* low-water
  mark, content-addressed dedup, spill-before-stub. The one context-
  proportional budget in the product. The model for Part 7.A's module.
- **`store/query.go:565`** — proportional share-out with floor, redistribution,
  in-budget clip markers, an overflow notice naming what did not fit. Exactly
  the right algorithm at exactly the wrong scale (4KiB).
- **Subharness registry** — one registration feeds the sizing enum and
  dispatch; unregistered names structurally unnameable; the grep law keeps
  "swe" out of the orchestrator. This is L3's seam, already enforced.

### 6.4 The defect ledger (ranked across all eight audits)

| # | Defect | Site | Axis |
|---|---|---|---|
| 1 | JIT expansion inline on the dispatch goroutine; a division ends the pass | `runner.go:533` | wall |
| 2 | `r.mu` held across compile+plan (charter firings), distill, replans, service probes — chat open and every question wait on it | `resident.go:554`, `watch.go:445` | wall |
| 3 | Class-blind provider limiter: one FIFO, capacity can AIMD to 1; an expansion that unblocks 20 leaves queues behind leaf retries; streams release at headers | `limiter.go:81`, `retry.go:108` | wall |
| 4 | Resident dep-context: 4KiB pot for ALL inputs, deps past ~8 dropped; headless gives 6KiB EACH; bundle sink told "every result in full" | `store.go:34`, `query.go:565` | quality |
| 5 | No write funnel: 87 `BEGIN IMMEDIATE` sites; rail check opens one per claim candidate, several/sec; provider calls end in sync writes | `store.go:584`, `usage.go:379` | wall |
| 6 | Sentinel holds `locks.pass` across its round-trip; ~10 serial post-work RTs on the worker slot | `chat.go:2934`, `chat.go:867-1286` | wall |
| 7 | JIT children: no edges (all inherit parent inputs, no sibling ordering), no brief, no criterion — thinnest orders where work was judged hardest | `expand.go:486`, `jit.go:448` | quality |
| 8 | Delivery gate parse failure ⇒ silent PASS; schema attached only when routed; head compiler hand-parses with no schema at all | `judge.go:660`, `compiler.go:371` | quality |
| 9 | Head wakes inline (45s each) on the one mail loop; in-process claim maps lose answers on restart | `head.go:322`, `absorb.go:278` | wall/quality |
| 10 | `aforge wake` has no runner — launchd-fired charters splice and stall | `wake.go:31` | quality |
| 11 | ~150 bare-literal context ceilings; exactly TWO call sites know any model's context size; no tokenizer, no overflow retry | Part 7.A table | quality |
| 12 | Second engine (`run`/`plan`/`revise`/`show` + `exec.Scheduler`): no gates, no JIT, no load gate, hand-synced with the resident path | `run.go`, `schedule.go` | all |
| 13 | Prompt case law at both ends: compiler 3,300 words / 27 bullets / magic-string trigger; gate 1,520 words of per-incident paragraphs; the split burden argument copy-pasted in 4 places (~1,200 words) | `compiler.go:25`, `judge.go:125` | cost/quality |
| 14 | Voice does gates' job: head directly mints/resolves questions, raises the rail by parsing a prose sentence out of a journaled message | `head.go:643` | quality |
| 15 | Ensemble is per-goal (dead in chat), never per-node; strategy choice cannot buy a panel on one judgment leaf | `plan.go:416` | quality |

---

## Part 7 — Three laws, measured against the code

### 7.A Context is for using — fill to 60–70%, then compact

**The law.** Any agent — head turn, planner pass, leaf, judge — fills its
model's window to **60–70%**, reserving completion room; only then does
compaction fire, and it compacts *properly* (summarize + spill with durable
pointers), never byte-truncates. Arbitrary small ceilings die. Nothing may be
half-informed by design.

**The measured gap.** The repo has ~150 bare-literal caps and exactly two call
sites that consult `catalog.ContextLength` for budgeting (both feeding
`Linear.WithContextLength`). No tokenizer, no prompt-size estimate, no
overflow retry outside the vendored SWE engine. Worst offenders:

| Cap | Value | Clips | Fate |
|---|---|---|---|
| `store.MaxDigestBytes` | 4KiB total | ALL dep inputs to a resident leaf | proportional pot per consumer model; keep `query.go` share-out algorithm |
| `exec.maxObservationBudget` | 64KiB | the one proportional budget, re-capped | **delete**; raise `/2` → 60–70% |
| `exec.boundInput` | 6KiB each | upstream results, headless | proportional |
| `jitDigestBytes` | 4KiB | what a sub-planner sees of landed deps | proportional |
| `stateResultsBytes` / per-result | 4KiB / 600B | the sentinel's whole world-view | proportional |
| `satisfied` tables | 600B ×24+24 | the growth governor's evidence | proportional |
| `head` per-message body | 600B unnamed | every thread message in the prompt | budgeted share |
| `beltResultBytes` + legacy read caps | 4KiB league | head reads (lens already pages) | **delete reads, keep lens paging** |
| `MaxMessageBytes` | 16KiB | one thread message — the craft-delivery autopsy's transport bound | spill to CAS + pointer (fold.go already does this shape) |
| judge MaxTokens | 200–400 | shared with reasoning-model deliberation; empty ⇒ silent pass | reserve-derived + **empty-reply retry** (plan.go:743 already has it) |

**The build.** One module — `internal/ctxbudget` — lifted from what already
works: `Window()` over the catalog (zero = unknown, never small);
`Budget{Context, FixedFloor, CompletionReserve, Fill%}` with `Share(weight)`
for composite prompts; compaction tiers = dedup (exec observations) →
spill+stub (exec decay, hysteresis + measured low-water) → **summarize**
(lift `swepro/.../session/compaction` — a complete, tested, 60%-of-usable
compactor with a structured summary template, today reachable only by SWE
leaves) → paging for chosen reads (lens part-N-of-M). Cache rules are the
hard constraint: hysteresis always; compact only contiguous oldest segments;
never rewrite a message a pointer names; never touch the system prefix.
`Budget.CompletionReserve` replaces `DefaultMaxTokens=32768` and the ~25 bare
MaxTokens literals. The numerator (`SpinePromptHighWater`) and denominator
(`ContextLength`) both exist today and have never met — the module is where
they meet, per call class.

### 7.B One headless surface: `aforge do`

`do` already rides chat's exact brain — reconciler + runner + consent +
gates + JIT + replan — behind the same funnel with the same admission. The
rest is a second engine and dies:

- **Delete**: `run`, `plan`, `revise`, `show` subcommands; `exec.Scheduler` +
  `exec.Registry` (+`registerLeafExecutors` dup); graph-file workflow; the
  bench arms that drive them (rewire to `do`).
- **Move, not lose**: `recordAndCalibrateWorker`/`Detailed` (the ruler loop —
  losing it silently kills learning), `spendPreauthorized`, `exec.LeafShape`,
  the verdict/escalation/timeout test coverage that exists only against the
  Scheduler.
- **`do` gains**: `-j/--workers` (unhardcode 32) · `--turns/--budget` ·
  `--graph out.json` (per-node verdicts for CI/bench) · TTY consent prompt
  (today `do` refuses where `run` asked) · `--dry-run` (compile+plan, print,
  claim nothing) · an explicit ensemble decision (flag it or delete the 600
  lines) · optionally fold `wake` in as `do --resident-pass`.
- **Close the wake hole**: whatever launchd runs must construct a runner, or
  scheduled work keeps stalling one stage before execution.

### 7.C Nothing blocks — async everywhere, the journal is the only queue

Ordered by bought wall-time:

1. **JIT off the dispatch goroutine** — expand owns its claim in its own
   goroutine; release the slot; re-nudge; never end the pass (`runner.go:533`).
2. **The reconciler tick sheds its tail** — charter compile+plan, distill,
   replans, service probes, reflect, consolidate move off `r.mu` (wrap in
   `thinking()` or a second goroutine); `r.mu` guards cursors only. Chat open
   stops waiting on a charter's planning pass.
3. **A real write funnel** — one writer goroutine + read pool
   (`ReadOnly:true` on read txs); rail check reads outside the tx; provider
   usage writes buffered; blob fsync and JSON marshal hoisted out of tx.
4. **Class-aware limiter** — sub-queues per `CallClass` with a reserved
   planner floor; release stream slots at watchdog close, not headers; route
   media through it.
5. **Sentinel coalescing** — landings enqueue onto the in-flight pass;
   pipeline the post-leaf judge chain; park (don't poll) service consent.
6. **Head mail loop** — wakes on their own goroutine keyed per node with
   durable claims; parallel read-only belt calls; per-room turn workers.
7. **Plan.Build pipelining** — contracts launch beside briefs; audit waits on
   bind only; per-expansion announce; panel overlapped or per-node at claim
   time; splice-then-retitle (title never gates admission); terrain rendered
   concurrently.
8. **Wider command admission** — distinct-target redirects are as independent
   as splices.

---

## Part 8 — Order of work

Each wave ships alone; earlier waves make later ones measurable.

1. **W-A Unblock** (7.C items 1–3): JIT off dispatch, reconciler tail off the
   lock, write funnel. Pure mechanics, no prompt or store-schema change.
2. **W-B Context** (7.A): `ctxbudget` module; kill the 4KiB league at the
   resident leaf seam first (defect #4 — biggest silent quality hole), then
   head blocks, then judges (+ empty-reply retry, fail-closed gate).
3. **W-C One funnel** (7.B): delete the second engine, grow `do`, close the
   wake hole. Docs and bench rewire ride along.
4. **W-D JIT completeness**: children get a STATE-only mini-bind, inherited
   criterion, real briefs; expansion digests go proportional (rides W-B).
5. **W-E Prompts**: hoist the 4× duplicated burden argument; rewrite compiler
   and delivery gate AGoT-style (graph vocabulary, zero domain nouns, schema-
   attached); per-node strategy verdict at claim time (revives ensemble as a
   panel option on judgment leaves). Friction: 98 `strings.Contains` prompt
   tests + 3 byte-pinned goldens.
6. **W-F Chat-side hygiene** (Part 2 backlog, now measured): delete the
   legacy 11 reads + hints/class/urgency empire; wakes durable; one door onto
   `CommandSplice`; gates logic out of voice; `compiler.go` out of
   `internal/head`; the engine out of `cmd/aforge/chat.go` into
   `internal/tasker`.
