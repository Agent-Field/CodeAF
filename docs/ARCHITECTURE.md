# Aforge as a resident agent — the finalized architecture

This document settles the long-running architecture: one permanent graph, one
lease-elected resident role, and any number of surfaces attached to it. It is
written as a set of decisions, each with the alternative that was rejected and
why. The current single-shot CLI (`aforge plan` / `aforge run`) remains a
supported mode throughout.

## The one-sentence design

A single, permanent, append-only task graph lives in `~/.aforge`; the file is
the truth, no daemon owns it, and any process may hold the one resident role
for that database while every terminal session, API caller, timer, or file
watch remains an attachment to the same graph.

---

## Decision 1 — The store: SQLite (WAL) for the graph, files for the bytes

**Decision.** The graph lives in `~/.aforge/aforge.db`, SQLite in WAL mode.
Large payloads — tool observations, artifacts, transcripts — never enter the
database; they live in a content-addressed store (`~/.aforge/cas/<sha256[0:2]>/<sha256>`)
and per-run workspaces, and the database stores pointers and bounded digests.

**Why not a custom binary format.** The instinct toward "binary or other clever
databases" is right about the workload but wrong about the layer. The workload
is: one elected mutation loop, many concurrent readers (attached terminals, the
TUI, sub-agents querying history), atomic compare-and-swap claims, and
queries shaped like "ready leaves under this subtree", "everything this session
spawned", "folds from this workspace". That is exactly a WAL-mode SQLite
profile — concurrent readers never block the writer, transactions give CAS
claims for free, and the graph becomes *queryable*, which a hand-rolled binary
file never is. SQLite comfortably holds 10⁶–10⁷ rows on a laptop; we are years
from that. The clever-binary energy goes where it pays: the **CAS blob store**,
which is git's trick (packfile-style content addressing for bulk bytes, small
index for structure). If event volume ever outgrows SQLite, the migration is
mechanical, because of Decision 2.

**Why not in-memory like plandb v1.** plandb v1 (aforge-v1) got the concurrency
semantics right — streaming admission, monotonic claim tokens, write scopes,
bounded digests — and this design keeps all of them. What it lacked was
durability: a process death lost the run (we lost a 19-minute benchmark run to
exactly this). The plandb API survives; its backing store becomes SQL.

## Decision 2 — Events are the truth; the graph is a view

**Decision.** The primitive is an append-only `events` table: `(seq, ts,
node_id, kind, payload)` — node spliced, claimed, turn completed, artifact
written, tokens spent, node settled, subtree folded, trigger fired. The
`nodes`/`edges` tables are a materialized view the resident keeps current in the
same transaction. Any state can be rebuilt by replaying events; resume after a
crash is "load view, continue", not "start over".

**Why.** Three problems collapse into one solution. *Resumability*: the silent
mid-run death costs nothing when every completed node's result is already
journaled. *History*: "what did the agent do last Tuesday and why" is a range
scan, not forensics over log files. *Attribution*: token/cost accounting is
read off events instead of inferred. The flight-recorder traces we bolted on
per-node become one event kind among many.

## Decision 3 — One spine, forever; goals splice, subtrees fold

**Decision.** The graph has a single permanent root. Every unit of work —
a user goal, a trigger firing, a task the agent sets for itself — splices a
subtree under it, stamped with provenance:

- `origin`: `user | trigger | self`
- `session_id`: which attachment asked (null for trigger/self)
- `intent`: the user's **verbatim words**, preserved unedited forever
- the grounding output: settled scope, open questions, evidence standard

When a subtree completes, it **folds**: one LLM call (the only place an LLM
call belongs in compaction — O(subtrees), not O(turns)) writes a bounded
digest of what was learned and produced, with pointers into the CAS and the
workspace. The fold node replaces the subtree in the *active* view; the full
subtree stays in the events table. The graph you look at is always small; the
graph on disk is the agent's whole life.

**Why verbatim intent.** Every pass we have (ground, fan-out, bind, brief)
interprets the goal, and interpretation drifts. The one thing that must never
be lost to compaction is what the user actually said. It is also the key for
memory recall (Decision 6).

## Decision 4 — One resident role per database; everything else attaches

**Decision.** The resident is a role, not an owning daemon. A process becomes
resident by taking a non-blocking OS lock on `resident.lock` beside the
database. The lock carries diagnostic identity only; SQLite events and views
remain authoritative, and the kernel releases the role on process exit.

An elected chat runs the head, reconciler, and workers. Other chat processes
stay surface-only: they tail the WAL-backed thread and append user messages or
command requests to its journal. `aforge wake` first probes the lease and
starts a bounded full reconciliation pass only when the role is free.

**Why a role instead of a daemon.** The file is independently readable and
writable under WAL, command requests are already durable mailboxes, and claim
tokens already make worker ownership a compare-and-swap. Electing the mutation
loop preserves one active scheduler per database without making availability
depend on a privileged process or a second control plane.

**Why a single scheduler thread is not the bottleneck.** Concurrency lives in
the leaves (each leaf is its own goroutine running its own model loop) and in
readers (WAL). The scheduler is a cheap event loop deciding readiness — plandb
v1 already proved this shape at `-j 6`. What was actually missing was not more
orchestrator threads but orchestrator *statelessness*: the resident holder may
die, restart, and pick up mid-graph from the store. Multi-writer
distribution (several hosts, one graph) is explicitly out of scope; if it ever
matters, the sharding unit is the workspace, and the event log makes
replication tractable. Do not build it now.

**Concurrency semantics carried over from plandb v1, verbatim in spirit:**
atomic pending→claimed CAS with a monotonic claim token (in SQL: `UPDATE …
WHERE status='pending' AND claim_token=?`), so a stale worker can never settle
a reassigned node; streaming admission so the first leaf starts before the
last is planned; write scopes so two leaves never mutate the same material
concurrently (the STATE edges the bind pass now emits map directly onto this);
failure is local — a failed dependency hands its digest downstream instead of
stranding the subtree.

## Decision 5 — Triggers are nodes the agent can plant

**Decision.** A trigger is a first-class node (`kind: trigger`) owned by the
resident trigger engine, carrying:

- a **condition**: `cron:` schedule | `file:` fsnotify glob | `webhook:` path
  \+ shared secret | `graph:` predicate (node settled/failed, spend threshold)
- a **splice template**: the goal text and workspace to instantiate when it
  fires (the template is re-grounded at fire time — the world may have moved)
- **rails**: a per-firing token budget, a firing-rate limit, and an expiry.
  A trigger without rails is invalid by construction.

Triggers are planted three ways: by the user (`aforge watch …`, `aforge cron
…`), by the planner (a graph can end in a trigger — "re-verify nightly"), and
by an executor tool (`plant_trigger`), which is the self-directed case: an
agent finishing a task can leave behind "re-run the suite when this file
changes". Firing is an event like any other, so trigger-spawned subtrees have
full provenance (`origin: trigger`, pointing at the planter).

**Why rails are non-negotiable.** A self-planting, self-firing agent is the
point of the design and also its main hazard. Every firing spends from a
budget fixed at plant time; the daemon additionally enforces a global
spend-per-day rail across all origins. Runaway is bounded by construction, not
by hoping the model behaves.

## Decision 6 — Memory is the folded graph, recalled at ground time

**Decision.** There is no separate memory system. Folds (Decision 3) *are* the
long-term memory: bounded digests with verbatim-intent keys, workspace keys,
and artifact pointers. The ground pass gains one step: query the store for
prior folds matching the new goal's workspace and intent (FTS5 keyword match
first; embeddings only if that proves insufficient), and inject the top few
digests as recall — "you have worked here before; here is what was learned,
and where the details live." The executor gains the complementary pull tool:
any leaf can query folds and read the pointed-at material from the CAS.

This subsumes the in-run knowledge index: a folded node's digest serves
siblings during the run and posterity after it — one mechanism, two ranges.
The capability profiles from the calibration plan live in the same store, keyed
by model and skill: capability memory and knowledge memory, same shelf.

## Decision 7 — The conversation is a lens on the brain, not the brain

**Decision.** There is one brain. A surface attaches to it and removes or adds
nothing but the person. `aforge chat` is that brain with a head and a terminal;
`aforge do` is the same construction with the conversation removed, and the
seam between them is exactly one thing: where the task comes from.

A chat ask travels through the head, which resolves what it points back into
before it becomes a command. A headless task is **verbatim** — there is no
conversation for it to point into, so it is referentially closed by definition
and goes straight into the journal the head would have written to. From that
command onward nothing downstream can tell which surface produced it, because
it is literally the same code.

**Why this and not a separate headless engine.** The alternative already exists
and is instructive: `plan`/`run` compiles a graph to a file and executes what
the file says. Everything this system learned about doing jobs happens *after*
the plan is written — the contract for the kind of work in front of it, the
gate that asks whether the person would accept this, the round a cited gap
earns, the replan when a leaf runs out of room. A frozen graph cannot do any of
it, so a second engine would either be a worse brain or a duplicate of this
one. Keeping `plan`/`run` for reading and hand-editing plans, and making `do` a
lens rather than an engine, is what stops the two from drifting.

**What the lens must still answer for.** With nobody watching, the process
itself has to say what a person would have seen: an exit code that separates
*failed* from *hit the wall* from *never attempted*, a question surfaced as
`blocked_on` rather than smuggled into the deliverable, and a periodic
structural read on stderr so silence is diagnosable. Those are contracts, not
conveniences — [HEADLESS.md](HEADLESS.md) is where they are written down and
what every harness is programmed against.

## Decision 8 — One boundary reads what a bad response meant

**Decision.** Three things used to arrive at the harness looking the same, and
every site answered for itself: the **transport** (nobody answered, or somebody
answered with something that was not an answer), the **capability** of the model
(a check read the finished work and named gaps), and the **work** (the job could
not be done, or the request itself is what is refused). `internal/taxonomy` is
the one place that tells them apart. `Classify(evidence, limits)` reads a small
evidence struct into one of the three classes and returns the single policy
registered for that class.

**The three policies.** *Transport* retries on the same tier with the endpoint
rotated underneath, N attempts doubling off one backoff — with **no** wait for an
empty 200 or a mangled tool call, which are instant failures from a healthy
endpoint that waiting does not mend. It never ends a turn and it never counts
toward a lift. *Capability* buys **one** tier after K findings on the same tier
with the wire ruled out, under a per-work cost cap, and hands the tier back the
moment a check passes. *Work* takes no action and is returned to the caller with
the evidence on it — which is where the landing machinery picks it up.

**Why.** Nothing about who *served* a request is evidence about who was *asked*.
On a five-run comparison the three runs that happened to roll four consecutive
malformed refusals read them as the model being unable, bought a model seven
times the price for the rest of the run and never came back down — 57–82% of
bills of $9.50–15.80, against $2.33 for the run that never rolled four.

**The knobs** are `internal/config`'s `ResponseLimitsAt`: `response.attempts`
(N, default 4), `response.lift_after` (K, default 1 — the count was never what
was wrong), `response.lift_cap_usd` (default $2 on a lifted tier per piece of
work), each with an `AFORGE_RESPONSE_*` pin. They are values in one struct, not
constants at the sites that need them.

**Where it is wired.** `internal/session/taxonomy_boundary.go` is the adapter and
the only file in that package allowed to call `Classify` or to buy a dearer
model. It is asked at the turn loop's retry ladder, at the turn loop's empty 200
(which no longer ends the turn), at the errand ladder's deadline, at the node's
model move after a run ends on a provider failure, and at the repair gate. Each
classification writes one journal line, `type: "failure"`, carrying the class,
the reason, the action and whatever evidence was there — so a bench counts the
ratio of transport to capability rather than reconstructing it.

**Why a registry and not a switch.** A switch on the class at each site is three
answers that start the same and drift the first time one is fixed. Two structural
tests hold the line (`internal/session/taxonomy_law_test.go`): `Classify` may be
called only from the boundary file, and so may the two functions that put work on
a dearer model. A new escalation trigger added anywhere else fails the build with
its line number.

## Decision 8 — Every session has a principal; unattended sessions get a Steward

**Decision.** One interface, `session.Principal`, is the addressee of every road
in the engine that ends in "ask the person": `Ask`, `Acceptance`, `Budget`,
`Report(landing)` and `Decide(remains) → {carry on with a brief | done | stop
with a reason}`. Two implementations. `Person` is the attended session and
**adds nothing** — it holds no acceptance, has no budget, turns no landing into
work, and decides exactly what `readRemains`'s empty string already decided.
`Steward` is the unattended one: `chat --yolo` **with a budget**.

**Why.** Autonomous runs were ending with most of their budget unspent, holding
partial work. The cause was not a bug in any function: it was a correct sentence
addressed to somebody who was not there. A landing that ran out of repair rounds
tells the model to "offer them a follow-up in their own words"; with no them,
the model answers in words, the turn ends, and the session idles. Nothing
anywhere held the whole ask, and nothing ever looked at the tree or at what the
session had left lying beside it.

**A budget is what arms it, and nothing else.** `--yolo` says one thing today —
run tools without asking — and reading it as permission to spend hours carrying
work on would be the harness acting on a sentence nobody wrote. `--max-hours`
and `--max-cost` (env `AFORGE_MAX_HOURS` / `AFORGE_MAX_COST`, either alone is a
budget) are that sentence. Without one, `--yolo` is exactly what it was and the
door prints one line saying what the other thing is called.

**What routes through it.**

| road | before | with a Steward |
|---|---|---|
| a stopped turn (`checkpointReopen`) | the mark reader's line, or the turn ends | the same line, plus the session acceptance, how the units of work landed, and — only when a principal says the ask is met — the declared checks re-run from clean |
| a landing that ran out of repair rounds (`taskNote`) | "offer them a follow-up" | `Report` turns the audit's own account of the gap into the next brief, in the same working copy |
| a landing nobody could judge (`settlePolicy`) | waits on a card | settles itself, as a headless run already did |
| the post-turn work judge (`routeJudge`) | skips every woken turn | a woken turn is judged against the session's frozen ask — the only kind of turn an unattended run has after its first |
| a standing item (`askStanding`) | "nobody is here to say yes" | the goal owner answers its own card, within the rails `Item.Validate` already demands |

**Session acceptance.** At the start of the first turn the judge's own machinery
(`routeVerdictContract`, `routeAcceptance`) writes one `done when` sentence for
the **whole** ask, journaled and frozen for the session — a done-condition the
work can rewrite is one the work grades itself against. Every acceptance before
this was one unit of work's, read only by that unit's auditor.

**The terminal audit.** Before a Steward may say done: re-run the checks the work
itself named (`declaredChecks`, the same reading a unit of work's auditor uses),
each in a fresh process in the deliverable tree; then reconcile everything the
session created. A created path inside the deliverable tree is part of the
answer; outside it, it is scratch, and scratch is removed and written down.
Nothing the session did not create is ever touched — the created bit is measured
before the call that writes the file and journaled, so it survives a resume — and
a `Person`'s session deletes nothing at all, it is offered the list.

**Rails.** The budget stops the run with a report rather than with silence. The
same failure signature three times stops it for good; the signature is the
audit's own first line today and is the field a proper failure classification
drops into unchanged. "Done" requires the acceptance to hold from clean, and a
session that has finished no unit of work is never done whatever its transcript
says.

**Why an interface rather than flags on the agent.** The two answers are a
policy, not a branch: a third principal — a person on another machine, a queue,
a scheduled owner — has to be writable without any road in the engine learning a
new name. Two structural tests hold the line: one fails when a new road onto the
wake queue appears without saying who it is addressed to, and one fails when a
person-addressed sentence is written anywhere that has never heard of a
principal.
## What this is not

- **Not a message bus.** Nodes do not talk to each other; they read folds and
  the CAS (pull), and the schedule orders mutations (STATE edges). Free-form
  inter-node RPC invites deadlock and context pollution for nothing the pull
  model doesn't provide.
- **Not a vector database.** Recall starts as FTS over digests keyed by intent
  and workspace. Add embeddings only when a measured recall failure demands it.
- **Not distributed.** One host and one lease-elected resident per database.
  The event log keeps the door open; nothing walks through it yet.
- **Not two products.** The headless command is a lens on the same brain, not a
  scripting-flavoured reimplementation of it. A feature that exists in chat and
  not headless — or that behaves differently there — is a bug in the seam.

## Migration map

| milestone | what lands | what it unlocks |
|---|---|---|
| M1 | `internal/store`: SQLite events + views behind the existing run; `aforge resume` | crash-proof runs, real accounting; the silent-death class of failure becomes impossible |
| M2 | resident lease + visitor surfaces; spine + splice + fold | universal cross-session graph; many-terminal attach; history queries |
| M3 | trigger engine + `plant_trigger` tool + rails | time/file/webhook/graph reactivity; the agent schedules itself |
| M4 | ground-time recall + executor fold-query tool | the agent that remembers its territory |

M1 is prerequisite to everything and valuable alone. Each later milestone is
independently shippable, and the one-shot CLI works unchanged at every point.
