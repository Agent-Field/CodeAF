# Subharnesses v2 — programs as the unit of repeatable work

*PRD, 2026-08-23. Status: **approved direction, not yet started**. This document is
the build brief for a lane (or several) to implement. It records decisions already
made with the owner — do not relitigate them, but do flag anything that turns out
to be impossible as written rather than silently building something else.*

*`file:line` anchors below were verified against `chat-v3-task` around commit
`ba6f7615`. This tree moves under several sessions at once — re-grep the anchor
rather than trusting the number if it does not land.*

---

## 1. What is being built, in one paragraph

A **subharness** is a named, versioned, *typed* program for a narrow recurring kind
of work — "weekly marketing for company X", "triage a flaky test" — that runs
inside the aforge binary, spends model tokens and belt tools only through host
calls the binary provides, presents to the person as a task, and is invokable
three ways: proposed by chat when a conversation matches its signature, picked
explicitly from a `/subharness` list, or run headless from the CLI. Some
subharnesses are written in Go and compiled into the binary (the defaults the
owner ships); everything a user builds is a JavaScript bundle in a store on disk.
**Neither the person, the model, nor the catalog can tell which is which** — the
contract is language-agnostic and the implementation language is a runner detail.

The theoretical frame, which shaped several decisions below: the generic agent
loop is an *interpreter*; a subharness is that interpreter *partially evaluated*
against a fixed domain. Everything known at design time (the sequence, the checks,
the house rules) becomes deterministic code; everything unknown until run time
(this week's input) remains as residual `ai()` calls. Failure handling borrows the
JIT's answer: compiled code runs behind *guards*, and a failed guard *deoptimizes*
to the general agent instead of erroring.

## 2. Vocabulary, and the two systems this replaces

"Subharness" currently names two unrelated systems that share not a line of code:

| | today | fate under this PRD |
| --- | --- | --- |
| `internal/exec` | Go executor kinds (`linear`, `swe`, `bare`) behind `Executor` (`internal/exec/executor.go:530`) and `SubharnessInfo` (`internal/exec/subharness.go:66`) | **absorbed** — these become the first Go-native subharnesses under the new contract |
| `internal/subharness` | LLM-designed JSON DAGs over ten node kinds, walked with one string of state (`internal/subharness/run.go:113`, `exec.go:184`) | **superseded** — the JS runtime replaces the DAG program form. The store layout, the design-task UX, the usage fold, and the salvage philosophy survive (§7, §11, §13); the node-kind language does not |
| `internal/orchestrate` | emergent frontier, no designed program | **untouched** — it answers the opposite question (one-off unknown-shape work). Subharnesses are for *recurring* work; recurrence is what makes a designed program worth having |

Migration posture: build the new system beside the old one; the old DAG designer
(`internal/session/harness_build.go`) keeps working until the new design flow
lands, then the `build_harness` verb re-points. Do not attempt an in-place rewrite
of `internal/subharness` — too much of it is the wrong shape.

Person-facing name is **subharness** everywhere (list, cards, manual). No
"specialist", no "program", no "bundle" in anything a person reads.

## 3. The contract

One manifest, language-agnostic, extending what `exec.SubharnessInfo` already is
rather than inventing a parallel type:

```
Manifest
  name          — the identity; one namespace across Go and JS
  purpose       — one line, drawn on lists and cards
  cues          — trigger vocabulary for matching (persisted; see §8)
  input schema  — JSON Schema; the typed front door
  output schema — JSON Schema; the promise the "done" surface renders
  whitelist     — belt tools this subharness may call
  cost shape    — prior anchors + deadline shape (keep the `linearInfo` form,
                  internal/exec/subharness.go:66)
  guards        — cheap preconditions checked before a run (§5)
  provenance    — built-in | yours | from this project
```

```
Runner
  Run(ctx, input, host) → output + report + artifacts
```

- **Go runner**: implements the interface natively, registered at compile time.
  The owner has custom Go-built subharnesses; they register here, first-class,
  zero porting. `linear` and `swe` are re-fronted through this registry.
- **JS runner**: ONE generic Go runner (the goja host, §5) parameterized by a
  bundle loaded from the store. The bundle carries the manifest.
- **Future runners** (subprocess, WASM): the contract permits them; do not build
  them now, and do not let their possibility complicate the two real ones.

Registry: one, unified. `exec.Registry.For` (`internal/exec/executor.go:570`)
already fronts executors with a name→runner lookup and a linear fallback — grow
that, don't duplicate it.

## 4. Typed input and output — the keystone

A subharness is a **function, not a chat**. Every downstream feature leans on
this:

- **Input** is a JSON Schema with defaults and optional fields. Go runners derive
  it by reflection over a struct; JS bundles declare it in the manifest.
- **Output** is a JSON Schema plus a short prose report plus optional artifacts
  (files). A run that ends without producing its declared output shape is
  *incomplete* — never "done with a strange message". This is the machine-checkable
  finish line the old one-string DAG lacked.
- **NL → input is chat's duty, made visible as an intake card** (§9). Chat fills
  the schema from the conversation; the card shows every field — filled ones
  stated, missing *required* ones highlighted. Grooming is **infer-then-confirm,
  never interrogate**: ask only for fields that genuinely could not be derived,
  batched into as few questions as possible. Fields with nothing render as
  nothing (the emptiness law).

## 5. The JS runtime

Add **goja** (pure-Go ECMAScript) to `go.mod`. The repo currently embeds no
interpreter of any kind (verified: no goja/starlark/lua/wasm in go.sum), so this
is a new dependency — pin it and note it in the PR description.

The sandbox IS the host API: goja has no filesystem, no network, no clock unless
the host hands them in. The entire capability surface:

```
ai(promptRef, input, {schema?, effort?})  — one model call; promptRef names a
                                            prompt asset in the bundle, never an
                                            inline string (§12)
tool(name, args)                          — the session belt, filtered by the
                                            manifest whitelist, through the same
                                            consent doors as any tool call
ask(question, {options?})                 — a human gate; headless behavior in §10
remember(note) / recall(query)            — subharness-scoped memory (its own
                                            file in the bundle store, not the
                                            session's memory)
log(status)                               — one visible progress row (task journal)
```

Rules:

- **Every token and dollar flows through `ai`/`tool`.** The program cannot spend
  any other way, so the existing `Usage` fold works unchanged — reuse
  `internal/subharness/usage.go` and its fold into the session
  (`internal/session/harness.go:180`, `foldHarnessUsage`). Honor its stated law:
  the ledger does not lock; if the runtime ever runs host calls concurrently, the
  ledger needs the mutex `usage.go:24-28` says it would.
- **Every host call is journaled** — inputs, outputs, cost — before its result
  returns to the program. The journal is the progress feed, the resume point, and
  the evidence for later revision. This is the same architecture as saved run
  traces today (`store.SaveRun`, `internal/subharness/store.go:217`) except the
  journal is *written incrementally* and *actually read* (§9, §12).
- **Fuel**: an operation-count interrupt (goja supports interruption) plus a
  wall-clock deadline from the manifest's cost shape, plus a token budget. Out of
  budget stops the run as *incomplete with a reason*, never hangs.
- **Guards and deoptimization**: before a run, the manifest's guards are checked
  cheaply (file exists, tool on belt, input field matches a pattern; at most one
  tiny model call). A failed guard — or an uncaught program error mid-run — does
  not fail the task: the run **falls back to the linear harness** with the
  original input, the journal so far, and a note of what broke. The person sees
  "step 3 needed a closer look — handled it the long way", in that register (§13
  vocabulary law). The deopt is recorded on the version's history; repeated
  deopts on the same guard are the signal that a revision is worth proposing.
- **Compile errors are the whole error story for authoring.** JS syntax errors
  carry line numbers; the model iterates against them. The four-rung JSON salvage
  ladder (`internal/subharness/salvage.go`) has no equivalent here and must not
  be rebuilt — do not accept almost-JavaScript.

One `Env`, two consumers: the host API handed to JS bundles and the interface
handed to Go runners must be the same Go type. The current tree has two drifting
`Env` implementations (`internal/subharness/exec_model.go` vs the session's) —
named in `cmd/aforge/chatv3_harness.go:41-53` as a known wound. Do not mint a
third.

## 6. The bundle

```
~/.aforge/subharnesses/<name>/v<N>/
  manifest.json      — §3, including cues (fixing the cue-death-at-restart hole,
                       named in internal/session/harness_build.go:414-421)
  program.js         — control flow; every block carries a required plain-English
                       desc (the projection source, §11)
  prompts/*.md       — every ai() call site names one of these
  memory.md          — accumulated domain notes, written via remember()
  evals/             — executable checks (v3; inert until then, but the directory
                       and manifest slot exist from v1 so bundles do not need a
                       format migration)
```

Versions are immutable and content-addressed: minted, never chosen — keep the
existing store's temp-file + `os.Link` exclusive-create discipline
(`internal/subharness/store.go:320`) and its no-head-file rule (highest page
present is head). Each version records its parent's hash and one line of why, so
a subharness's lineage reads like a log.

## 7. Storage layers and distribution

Lookup order at run time, first hit wins:

1. **Packed trailer** — a zip appended to the binary itself (Phase 2). Executable
   formats ignore trailing data; `aforge pack <names...>` emits a new file that is
   the same binary plus a zip of bundles plus a manifest — no Go toolchain, no
   rebuild, a single versioned, hashable deploy artifact for headless boxes.
2. **Project store** — `.aforge/subharnesses/` inside the repo. This is the
   org-sharing story and it needs zero infrastructure: sharing is `git pull`,
   review is a PR, versions are history. Build nothing registry-shaped.
3. **Home store** — `~/.aforge/subharnesses/` (moves with `AFORGE_HOME`, like
   today's harness store).

Go-native subharnesses are layer 0 implicitly: compiled in, always present.

## 8. Discovery — one mechanism for every dynamic collection

This section is deliberately general. The binary now has several *dynamic,
unbounded collections* competing for context: belt tools, subharnesses, and
whatever comes next (saved prompts, skills). The owner's requirement: **each kind
gets its own inline limit, and everything beyond the limits is handled by one
unified mechanism** — not a per-kind special case.

The frame that makes this clean: **context is a cache over the capability
universe.**

- **One index.** Every dynamic item — every tool, every subharness — registers
  one entry: kind, name, one-liner, tags (from cues), signature if typed,
  last-used, use-count. Go-native and JS entries are indistinguishable here.
- **Partitioned inline budget.** Each kind declares a slot count (e.g. tools
  ride fully on the belt up to ~20; subharnesses get 2–3 reminder slots per
  turn; a new kind declares its own number). What fills the slots is an
  **admission policy shared across kinds**: relevance to the current turn
  (lexical cue match + type-directed match, below) blended with warmth
  (recently/frequently used items keep their slots — an ARC-style balance of
  recency and frequency, so one-off matches don't evict the daily driver).
- **One search verb for every miss.** A single belt tool — `catalog(query,
  {kind?, tag?})` — is the door to everything not inline. Its result shape makes
  the search *recursive by construction*: when matches fit, it returns items
  (name, one line, signature); when too many match, it returns **tag buckets
  with counts** ("41 match 'marketing' — email 12, social 9, launch 7, …") and
  the model drills into a bucket with the same verb. Bounded depth (3), bounded
  result size, no paging.
- **Type-directed match** is the second routing signal and the one cue-matching
  cannot fake: what does the conversation *hold* (a brief, a failing test name,
  a CSV) and what is being *asked for* — matched against input/output schemas,
  Hoogle-style. Cheap, mechanical, high-precision.
- **Manual synergy — this is load-bearing.** Every manifest auto-generates a
  manual page (purpose, signature, cues, limits) at registration, into the chat
  corpus (`internal/manual/chat/`). The three manual gates then pass by
  construction for all sixty subharnesses, "what can you do?" answers correctly,
  and — the point — **the manual's retrieval engine and the catalog index are
  the same index**. The retrieval machinery already exists; cues are headings.
  Do not build a second search engine.

Per-turn: the top-k subharness candidates (name + one line + signature, nothing
more) ride into context as a quiet reminder only when relevance clears a
threshold; silence otherwise. The model always has *awareness* of a few and
*access* to all.

## 9. Launch paths and the intake card

Three doors, one card:

- **Auto-propose** (chat or an adaptive run decides): follows the `propose_task`
  consent pattern (`internal/session/tools_harness.go:48` keeps the three verbs
  disjoint). Chat says why it matched — "this looks like `flake-triage`: the
  brief and a failing test name are both here" — and raises the **intake card**
  with the countdown door. Confidence gates behavior: high → propose with a
  filled card; medium → mention as an option in prose; low → silence. **Never
  silent auto-execution** — the card is the consent.
- **`/subharness`**: opens a filterable list — type-to-filter over
  name/purpose/cues; each row: name, one-liner, cost shape, dim provenance mark,
  dim last-run note if history exists (nothing otherwise — emptiness law).
  Enter → the same intake card, pre-filled from the conversation.
  `/subharness <name>` jumps straight to the card. Register the command and its
  aliases in the manual or `internal/tui3/manual_test.go` fails the build.
- **Headless**: `aforge run subharness <name> --input <file.json|->`. No task
  surface, no cards; `ask()` behavior is declared per-gate in the program —
  either a manifest-defaulted answer or refuse-and-stop-incomplete. **Never
  silently auto-approve**; the old bridge's auto-approving gate
  (`internal/subharness/exec_model.go:63-72`) is the anti-pattern, kept honest
  there only by writing it into the trail.

The intake card is one card, both interactive paths: every input field, filled
ones stated, required blanks highlighted; the person edits inline or answers
chat's (batched, minimal) questions; confirm launches. Reuse the
harness-approval-card muscle (`internal/session/harness_task.go`, the
`reviseDoor` flow) — same shape, different payload.

**A run is a task node**: roster row, room, journal (fed by `log()` and the host
journal), an id, a ✕ (cancellation via the existing `Cancel("harness:<id>")`
route, `internal/session/cancel.go:270-301`). This fixes today's asymmetry where
*designing* a harness is a task but *running* one blocks the conversation as a
turn (`internal/session/harness.go:129`). The task surface is a *presentation*
of a run, not its definition — the headless path must not import the task
system.

## 10. Subharnesses inside the task graph

Owner requirement, verbatim intent: a subharness node sits in the task graph
**with upstream and downstream edges to normal (prompt-driven) task nodes**, like
any other node.

- **Upstream** (a normal task feeds a subharness): when the edge fires, the
  upstream output — prose, files, whatever — is handed to a small adapter step:
  one model call that fills the subharness's input schema from that material plus
  the original conversation context. Missing required fields at edge-fire time →
  the intake card rises then (or, if the graph was designed interactively, the
  designer can pre-declare "this field comes from upstream" and the card was
  already settled at design time).
- **Downstream** (a subharness feeds a normal task): the typed output travels as
  **text plus its schema** rendered into the downstream prompt. No rigid typed
  plumbing between nodes — the owner explicitly chose loose coupling: pass the
  JSON as text, let the model reshape it for the next consumer, ask the person
  only when something required is genuinely absent.
- **Chains of subharnesses** (subharness → subharness) are the same two rules
  composed, and are **Phase 2** — the signatures make them cheap to add; do not
  build chain-specific machinery in Phase 1 beyond not blocking it.

## 11. Design-time UX — the split pane

The co-design flow replaces the one-shot 25k-token DAG designer with a
conversation, and its centerpiece is a **projection, not a translation**:

- The design session runs as a task (keep the existing design-task shape: a room,
  a thread agent, phases on `TaskNotice.Doing`, the 30-minute-per-page window and
  the card-never-expires fix, `internal/session/harness_build.go:74-100`).
- The surface splits: **left is the conversation, right is the program rendered
  as numbered plain-English pseudo-code** — indented by real control structure,
  each line drawn from the *required `desc` annotation* on the corresponding
  block of `program.js`. One source of truth: the pane renders only what the
  code carries, source-map style, so it cannot drift or lie. It is never a
  separately-written summary.
- **Say it, see it become a step**: as the person talks, the model patches the
  program and the pane re-renders live. "Always show me the email one first"
  visibly becomes a gate at step 2b. Patch-sized edits, not whole-page re-emits —
  the review-by-patch rationale (`internal/subharness/ops.go:6-27`: a critic
  re-emitting 4KB retypes "whisper.cpp") applies doubly to code, where the
  natural patch form is a diff.
- Direct manipulation for small structure: cursor onto a step to mark "check
  with me here" (inserts `ask()`), drop it, reorder. Conversation for content,
  keys for structure.
- **The same pane is the run-time view**: during a run the identical projection
  renders with the current step lit, done steps dimmed, a deopt shown as "needed
  a closer look — handled it the long way". Design view, review card, and
  progress display are one representation the person learns once.
- Marks stay in the v1 visual register (restrained, dim, no borders —
  `internal/tui` is the north star): a dim glyph for costs-tokens (✦) and
  asks-you (◦), nothing for deterministic steps.

Pane mechanics are the largest UI lift in this PRD; it is acceptable to land
Phase 1 with the projection rendered as a static block in the design room
(re-sent on change) and make it a true live split pane in Phase 2.

## 12. Improvement over time — scoped, in order

Explicitly **not** an autonomous self-improvement loop. Three mechanisms, cheapest
first, later phases:

1. **Per-call-site inline caches** (Phase 3): each `ai()` call site keeps a small
   cache of *accepted* outputs (kept by the person, or downstream-consumed
   without complaint) as its own few-shot examples — warmed by use, evicted by
   staleness. Improvement with zero machinery beyond an append on approval.
2. **Deopt-driven revision proposals** (Phase 3): repeated deopts on the same
   guard raise a card — "step 3 keeps needing the long way; want me to widen
   it?" — and an accepted revision mints v(N+1) through the normal design flow.
   The person is always the door; the `selfmod` idea from the old dynamism
   ladder stays locked.
3. **Executable evals** (Phase 3): the `evals/` directory runs before any
   version is minted. The old system's `Tests` field was validated, drawn on the
   card, and never executed — the single clearest lesson to not repeat.

Run journals are saved from Phase 1 **and read from Phase 1** (the run view and
the "how it went" line on the list) — the old system saved every trace and had
zero readers (`Store.Runs`: no non-test callers), which is how learning quietly
became impossible.

## 13. Laws this build is subject to

All from `CLAUDE.md` and the tree; the gates are real and fail the build.

- **The manual law**: every new slash command (with aliases), every new belt verb
  (`catalog`, the run verb), every limit and refusal lands in
  `internal/manual/chat/` in the same change. §8's auto-generated pages cover
  the per-subharness surface; the *feature itself* (what is a subharness, how do
  I build one, what can't it do) needs hand-written pages. When this makes
  something possible that a page currently denies, hunt down and fix the denial.
- **The emptiness law**: no history renders as nothing, not "0 runs".
- **Vocabulary**: no `auditor`/`verdict`/`verified`/`refuted` anywhere a person
  reads. Work is running, finishing, done, incomplete, or needs your look. Guard
  failures and deopts especially — "needed a closer look", never "guard failed".
- **Absent, not broken**: over a remote connection, follow the precedent at
  `cmd/aforge/engine.go:99-115` — if design-over-remote cannot raise its card,
  the verb is absent, not present-and-failing. Same for any capability a given
  door cannot support.
- **One source of truth**: the pseudo-code pane is a projection (§11); schema
  defaults are stated once and interpolated; the system prompt
  (`internal/session/prompts/system.md`) must not promise conditional verbs
  unconditionally.
- **Generic and meta, never hardcoded**: aforge ships the runtime, the contract,
  the index, and the loop — zero domain content. No hardcoded subharness lists,
  no if/else routing rules; matching is data-driven from manifests.
- **Multi-session etiquette**: build in a worktree off `chat-v3-task`
  (`git worktree add ~/af-<name> -b <branch>`), never `git add -A`, re-run
  `go build ./...` after fetching. `go test ./internal/tui3/` takes ~150s; the
  known-failing and known-flaky tests listed in `CLAUDE.md` are not yours.

## 14. Phases and acceptance

**Phase 1 — the spine.** Unified registry + manifest (Go runners: `linear`,
`swe`, plus the owner's custom Go subharnesses); goja runtime with the five host
calls, journal, fuel, usage fold; bundle store (home layer only); typed I/O;
intake card; `/subharness` list + card; auto-propose behind the consent card;
run-as-task-node; headless `aforge run subharness`; deopt-to-linear on guard or
error; manual pages. *Accepted when*: a JS bundle authored by hand runs from all
three doors, spends through the ledger, shows as a task, falls back to linear on
a forced error, and every manual gate passes.

**Phase 2 — scale and reach.** The capability index + per-kind budgets + the
`catalog` verb with recursive tag drill-down; auto-generated manual pages wired
to the same index; project store layer + git sharing; `aforge pack`; task-graph
edges (upstream/downstream adapters) and subharness→subharness chains; the live
split-pane design view; the co-design flow replacing the DAG designer, with
`build_harness` re-pointed. *Accepted when*: 60 seeded manifests route correctly
(right top-k inline, drill-down finds the rest), a packed binary runs its
trailer bundle on a clean machine, and a two-node graph (prompt task →
subharness) completes with the adapter filling the intake.

**Phase 3 — compounding.** Inline caches per call site; deopt-driven revision
cards; executable evals gating version mints; richer run-history surfaces.

## 15. Decisions already made (do not reopen)

- The name is **subharness**; runs present as tasks; the contract stays
  independent of the task system so headless works.
- JS (goja) for user-built; Go compiled-in for owner-shipped; the person never
  needs to know which is which.
- Typed input and output; chat translates NL and grooms via the intake card.
- Loose coupling on graph edges: JSON-as-text plus a model-mediated adapter, not
  rigid typed plumbing. Chains are Phase 2.
- Discovery: per-kind inline limits over one shared index with one recursive
  search verb; tags/cues; type-directed matching.
- `internal/orchestrate` is untouched; the old DAG program form is superseded,
  its store discipline and design-task UX are kept.

Open (small, decide during build, note the choice in the manual): the exact
per-kind inline slot counts; the relevance threshold for auto-propose
vs. mention; whether the Phase-1 projection is a static block or already a pane.
