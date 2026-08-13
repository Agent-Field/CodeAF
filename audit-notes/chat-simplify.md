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

1. **Chat is intent capture and reporting — never execution shape.** The
   person speaks in outcomes ("make 10 reports instead of 2", "don't do
   this"). The chat's job is to bind that intent to the right object and the
   right verb, and *nothing else*. What the intent means for a plan is the
   tasker's interpretation problem.
2. **There is exactly one seam between chat and work: the command funnel.**
   Chat never talks to a planner, a sentinel, a worker or a craft directly.
   It journals a command; the tasker side interprets it. This seam already
   exists (`RequestCommand` → reconciler) and is the one piece of the current
   architecture that is exactly right.
3. **The person never needs a verb vocabulary.** They say what they want in
   their own words. The model needs a *small* verb vocabulary — and the
   smaller it is, the less it can fumble.
4. **Nothing the system knows is unreachable from chat.** The journal is the
   truth and the conversation is a lens over it (Decision 7) — so the lens
   must be total: live work, a running step's own progress, any finished
   result, any file, anything ever said or learned, and the system's own
   health and spend. If the store knows it, chat can surface it.
5. **The reply is written for the reader, sized to its content.** Raw is for
   the loop's own reading; composed is for the person. "Task done" is never
   an answer; a reply cut at an arbitrary token ceiling is never an answer
   either. The last mile — from settled work to spoken answer — is a real
   turn with real reads, not a relay.
6. **Defaults are learned, not legislated.** Where the model faces a genuine
   judgment call (do this here vs. hand it over), the answer is: judgment
   first, a cheap structured question when genuinely unsure, and a learning
   loop that turns repeated answers into defaults — using machinery that
   already exists.

Standing assumption (confirm if wrong): the chat loop stays seconds-scale.
"Multistep" means reads, recall, small acts and asks — not minutes of compute.
Anything needing a workspace and time is the tasker's, always.

---

## Part 1 — Where the complication actually is (measured)

### 1.1 The prompt is case law

~14,000 chars system prompt + ~22,000 chars of tool descriptions = **~9,000
tokens fixed, every message**. Inside it:

- **50 "never", 1 "must".** The law section (5,008 chars) is 3× the job
  description (1,734 chars).
- Every law is a memorial to one incident (bd3c78ed's SVG, the double stock
  job, "I've put it in hand"). Real failures — but the median message is
  "open the pdf", and it pays the full catastrophe tax.
- The doctrine is stated twice: once in `orchestratorLaw`, again inside the 29
  tool descriptions (spawn's alone is ~250 words of policy). The old
  router-vs-belt split brain reborn as prompt-vs-descriptions.

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
- *"make 10 reports instead of 2"* → `change(job, words)`; the revision judge
  replans the remainder and tells the running workers.
- *"broadcast? planner? sentinel?"* → never chat's question. Chat binds
  intent to target; the funnel is the only seam; planner and sentinel are
  internal organs of the tasker with no chat-facing names.

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

1. **Receipt** — settles every command. Structured: what was understood (the
   reading), the shape chosen (steps, craft used or not), the estimate and
   cap, what a change actually did to the plan (for `change`), what was
   withdrawn (for `stop`). The voice's receipt-wake turn (2.8A) renders it in
   its own words; the raw row remains for `open(raw:true)`.
2. **Progress** — a duty, not an event: every job root owes the journal
   periodic plain-language progress lines (largely exists as the task-room
   feed today; the contract makes it required and person-readable, because
   `open` on a live job serves it to the head).
3. **Delivery** — a finished job: summary, files with paths, spend, how parts
   ended, and *what fell short* if anything did. The delivery turn that
   consumes it is specced in 2.6.

Two things move *into* the tasker from the head, because every surface should
benefit and chat should carry no work-shaping logic:

- **Admission control** — `pendingTwin` / duplicate-ask dedupe runs at the
  funnel door, so a headless `aforge do` gets the same protection chat gets.
- **The whole interpretation of change** — the revision judge formally owns
  steer-vs-replan-vs-expedite; chat's `change` is words + target, nothing
  else.

### 2.6 The answer contract — the last mile

The raw question has two sides, and they get opposite answers:

**Retrieval — raw in.** The loop reads actual bytes: results whole, files
whole, journal rows when asked. Paged, never silently clipped (2.2). The
model cannot compose a good answer over a 4KB peephole, so the peepholes go.

**Response — composed out.** Everything the person sees is written for them,
by the voice, sized to its content. The raw output of work is a record the
lens can always serve — it is not the message.

What that means concretely:

- **The delivery turn becomes a real turn.** Today's absorb is toolless,
  capped at 600 tokens over a 4KB peek, and *forbidden* to restate the result
  because the raw result is pasted verbatim underneath. Inverted: when work
  settles, the head is woken with the structured Delivery **and its lens** —
  it opens the result, reads the file if that is where the substance lives,
  and answers the person's original ask: the findings, the verdict, the
  numbers — then the pointer: "full report at ~/reports/q3.pdf". The verbatim
  result row stays journaled as the record (the task room renders it; `open`
  serves it), but the room's *answer* is the head's composed reply. "It's
  done" is never an answer; neither is a pasted log with one sentence on top.
- **A file the head wrote itself needs no re-read** — it authored the bytes
  this turn; it answers with the substance and the path in the same breath.
- **Trivial things are done, not dispatched and not narrated.** "Open the
  pdf" is `act` and one line back. The boundary (2.7) keeps trivial asks out
  of the tasker; this contract keeps the *tone* right — the interface behaves
  like a person doing a small favor, not a system logging a dispatch.
- **The reply caps die.** `orchestratorMaxTokens` (1,200) and
  `absorbMaxTokens` (600) were truncation-by-design and they cut real
  answers. What replaces them is judgment plus the guards that actually
  worked: the artifact law as a principle (document-shaped things go through
  `write`), the truncation *mark* mechanism for provider-side cuts (12.5 —
  any cut is visibly a cut), and a generous runaway bound in the loop (calls,
  not words). A reply as long as its content is correct; a reply cut mid-SVG
  was the defect the law was written about — keeping the cap keeps the
  defect, self-inflicted.

### 2.7 The delegate boundary: floor → judgment → ask → learn

The one place the model must be very clear: real work goes to the tasker.
Four layers, none of them a keyword list:

1. **Floor (code).** Inline hands are structurally incapable of work: `act`
   is one command, seconds, reversible, consequence-gated — and its refusal
   names `task` as the route (all already true). Nothing in the loop can
   produce a deliverable except `write`, and nothing can take minutes.
2. **Judgment (prompt, ~4 sentences).** *"You do two things yourself: find
   things out, and instant reversible acts. Anything that produces a
   deliverable, takes real time, or can't be undone is work — hand it to the
   workforce as ONE task, written richly: their words, the context that
   settles what 'done' looks like, the constraints you both know. You never
   split work into pieces; the workforce decomposes. When you genuinely can't
   tell whether they want the quick look now or the proper job, ask."*
3. **Ask (mechanism, cheap).** The existing `ask` tool: one numbered
   question, options as clickable rows, turn ends, answer returns through the
   deterministic gate. Phrased in outcomes, never machinery: not "should I
   make this a task?" but "Quick answer from what I already have, or a proper
   researched report?"
4. **Learn (existing machinery, new category).** `ShouldAsk`
   (`store/meta.go:561`) already implements VOI-learned ask-or-assume per
   question category with consent-bearing exceptions;
   `RecordAssumedWithDefault` already journals skipped asks so corrections
   match back. Register `scope` as a category: early boundary asks are real
   questions; consistent answers collapse into a declared default; durable
   per-person patterns get a `note`. Zero new learning architecture.

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

1. **Who you are** — a person with a job, not "the orchestrator of a
   task-graph agent". The identity carries the voice.
2. **What you have** — the four reads, the three work verbs, hands, the
   notebook. Six sentences.
3. **Judgment** — the boundary paragraph (2.7.2) plus:
   - *Ground every claim in something a tool showed you this turn.*
   - *Deliverables are files; conversation is meaning.*
   - *A follow-up about work in flight changes that work; a genuinely new ask
     is new work; when several things match, ask.*
   - *Numbers are quoted, never derived.*
   - *An honest miss beats a fluent reconstruction.*
4. **Gates** — one paragraph: a gate may stop a change and ask; the question
   is the reply; consent questions belong to the person.
5. **Voice** — a handful of positive lines: the first sentence answers; size
   the answer to its content — substance first, pointer after; when work
   settles, answer the ask it was commissioned for, never report completion;
   speak in their vocabulary (one sentence; `open(raw:true)` on explicit ask
   is the sanctioned exception).

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

1. **Prompt + tool-description rewrite.** No mechanism change. Delete the six
   code-enforced laws, collapse descriptions to interfaces, rewrite desk and
   voice, add the boundary paragraph. Guard with the golden-turn/journey
   suites. Removes ~7k fixed tokens.
2. **The lens: `recall` + `open` + `status`, paged reads.** Additive store
   views over the five FTS indexes and the live feeds; old read tools stay
   wired until parity, then drop out of the belt. Delivers Part 0.4 (total
   observability, including live node detail) and the retrieval half of the
   raw policy.
3. **Receipt wake + interim speech + the answer contract.** The delivery
   turn gets the lens and loses its caps; reply caps die; truncation marks
   stay. Then delete `await`, the read cap, the spent strings, `promise.go`.
   Type the Receipt (2.5.1) in the same wave — the wake turn is its consumer.
4. **The verb triad.** `task`/`change`/`stop` land; `orders`/`fanOutLimit`
   die; amendment door becomes `amends:` + judgment; dedupe moves to the
   funnel; steer/revise/expedite merge behind the revision judge.
5. **`scope` question category** wired into `ShouldAsk` +
   `RecordAssumedWithDefault`; boundary asks start learning.
6. **Lexical deletions** (hints, class NL, urgency, cue halves) once 4–5 are
   proven against the journey suite.
7. **Module extraction** — compiler out of `internal/head`, engine logic out
   of `cmd/aforge/chat.go`, packages named voice/lens/tasker/gates.

Steps 1–2 are low-risk and reversible; 3 is the payoff; 4–6 ride on 3; 7 is
hygiene that gets cheaper after the surface shrinks.

---

## Part 4 — Decisions taken, and what's still open

**Decided (this revision):**

- **Raw in, composed out.** Retrieval is raw and paged — the loop reads whole
  results, whole files, journal rows on request, never a silent 4KB clip.
  Responses are always composed for the reader; raw output reachable via
  `open(raw:true)` on explicit ask.
- **The delivery turn is a real turn** — lens-armed, uncapped, answering the
  original ask from the actual deliverable, pointer to files after substance.
- **Reply token ceilings are removed**; the guards that stay are the write
  tool, the truncation mark, and a call-count runaway bound.

**Still open:**

1. **`change` with no verb at all** is the boldest cut. Fallback if the
   revision judge misreads too often: `change(target, words, hint?)` with
   hint ∈ {constraint, replan, sooner} — still one tool, hint is evidence.
2. **Does `stop` fold into `change`?** Kept separate: withdrawal is
   consent-gated and definite — a destructive intent deserves an unambiguous
   door.
3. **Does the chat loop ever get minutes-scale hands?** Assumed no. If yes, a
   third lane (a "session job" the loop owns) is a different design.
4. **JOURNEY.md** is product law (one mouth, 19 journeys). Steps 1–6 should
   be provable against the journey suite as-is; where a journey encodes old
   tool names, the suite moves with step 4, not before.

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

1. Threads talk to the colleague; task pages talk to the work. Two talking
   surfaces, one became plural.
2. Creation and splitting are conversational offers, never chrome. The only
   deliberate gesture is typing.
3. One ornament: the unseen-delivery dot. No badges, counts, or competing
   color.
4. Naming is the scribe's job, silently; "call this thread X" works as a
   change.
5. The list is a switcher, not a manager: open, filter, enter. Nothing to
   organize, tag, or archive. Threads never close; they go quiet and sink.

### 5.2 The journeys (J1–J7, join the journey suite)

- **J1 split-on-divergence**: mid-conversation pivot → head offers "own
  thread?" as a numbered ask (category-learned like scope; consistent yeses
  collapse into acting with one clause said). Transcript breathes one ruled
  line + title chip; you are already talking.
- **J2 working session**: ideate → task commissioned FROM the thread → keep
  talking while it runs → delivery returns INTO the thread → more tasks. The
  thread is the desk; 1 thread : N tasks.
- **J3 switching**: one key (`t`) or click the title chip → quiet overlay
  list: name, "left at:" line, relative time, ● only where something landed
  unseen. Typeahead, enter, esc.
- **J4 re-entry brief**: first turn after a gap opens with the head speaking
  the arc — what this thread is about, positions taken, tasks run and what
  they returned, what was left open. Journal-derived, rebuildable.
- **J5 alive glance**: "what's going on" answers running work AND open
  threads in one breath; threads with unresolved arcs are alive.
- **J6 nudge**: parked-thread mention on the presence line / first morning
  exchange; the response teaches cadence (note/scope machinery).
- **J7 no lifecycle**: threads sink when quiet, recall reaches them forever;
  months later the head finds the old thread and offers it before starting
  fresh.

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
| attribution | task page adds "for: <thread>"; activating it jumps to the thread | ran-by row |

### 5.4 The one cross-seam contract

The split answer must move the SURFACE to a new session, and the coupling
stays journal-only: the deterministic answer gate settles the split by
posting a system row in the old thread ("continuing in <name>") carrying a
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
