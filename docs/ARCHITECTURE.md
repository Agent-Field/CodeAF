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
