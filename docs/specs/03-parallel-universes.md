# agit — Document 3: Parallel Universes — Many Agents, One Repo

**Status:** Draft 0.1 — companion to [Document 1](01-working-state.md) (engine semantics — authoritative where they touch) and [Document 2](02-distributed-runtime.md) (distributed runtime and commercial model)
**Scope:** The product's primary theme — replacing `git worktree` for the agent era — and the capability sets that deliver it: true parallel universes (A), free convergence (B), free capture (C). Distributed universes (D) are *recorded here and deliberately deferred*. Also: the CLI/agent-experience design discipline and the single sanctioned UI.
**Last updated:** 2026-07-09
**Phase footprint:** Nothing in this document touches Phase 0/1 scope. Engine work in progress is unaffected.

---

## 1. Positioning

> **Worktrees were built for one human switching branches. agit is built for many agents sharing one repo — with instant forks, live conflict radar, and merges that are already done when the agents finish.**

This is the main theme. It displaces a tool people already use and already resent (`git worktree`: tracked-files-only, N× dependency installs, N× disk, port collisions, merge dread) at the exact moment parallel-agent workflows are exploding. Rewind remains in the product as the trust feature — "every universe is rewindable" — not the headline.

Everything in this document serves one sentence of user experience: *start N agents on one repo in one command, watch them not collide, and accept an already-verified merge at the end.*

---

## 2. Capability Set A — A Fork Is a True Universe, Not a File Copy

### A1. Same path, different universes (`agit exec`)

```text
agit exec --fork a -- claude -p "fix the auth bug"
agit exec -n 3 -- <cmd>          # three universes, forks created implicitly
```

On Linux, each spawned process runs in a **mount namespace** where the canonical repo path (`~/project`) *is* its fork's materialization. The agent believes it is in the one true directory; each is in its own universe. No `cd ../project.forks/a`, no path drift in agent prompts, no tool breaking because the path changed.

- Implementation: unprivileged user namespace + bind mount of the fork materialization over the repo path, per process tree. Requires kernels with unprivileged userns (mainstream since ~5.11; detect and report).
- macOS: no mount namespaces — `exec` falls back to sibling directories with the fork path exported as `$AGIT_WORKDIR` and injected as the process CWD. Honest per-platform behavior, disclosed by `agit exec --plan` (consistent with Doc 1 Invariant 11).
- `exec` is the flagship verb: it *subsumes* manual fork management for the common case (fork creation, namespace setup, env injection, cleanup on exit are implicit).

### A2. Port and service virtualization per universe

The collision after files is ports: N dev servers all want `:3000`.

- Linux: each `exec` universe gets a **network namespace**; every agent binds `:3000` privately. agit maps each universe's ports to distinct host ports (veth + forward) and reports the table.
- macOS: no netns — `exec` injects `PORT`/well-known env offsets per universe and runs a small local proxy for the common frameworks. Weaker, disclosed.
- Surfacing: **not a new command.** Port mappings appear in `agit forks` output and in the MCP `fork_info` result.
- Fork hooks (Doc 2 §2.2 / Doc 1 §8.2) remain the escape hatch for databases and services that need per-universe renaming.

### A3. RAM dedup — the low-memory enabler (benchmark claim, not a feature)

Reflinked forks share extents; the OS page cache holds **one** copy of shared bytes across all universes. Ten agents' builds against ten forks of `node_modules` use approximately the disk of one and the RAM of one. There is nothing to build here beyond Tier-0/2 forks (Doc 1 §8.2) — but it must be **measured and published** as a headline benchmark: *"10 universes, ~1.0× page-cache footprint."* This claim is why "10 agents on a laptop" is physics, not marketing.

---

## 3. Capability Set B — Convergence Is Precomputed

The daemon is the only process with a live view of every universe's dirty paths and op journals. That vantage point — not any single algorithm — is the moat of this section.

### B4. Conflict radar

- Mechanism: continuous set-intersection over per-fork dirty-path sets and claim tables (Doc 2 §4). Cost: trivial.
- Human surfacing: a `conflicts` column in `agit forks`; nothing else.
- Agent surfacing: `fork_conflict` events on the `agit events --follow` NDJSON stream (path, forks involved, claim state — mirrored to MCP notifications per §6.3) and a `coord`-class advisory file — so agents *route around* collisions instead of creating them. Radar events suggest claims; claims remain advisory (Doc 2 §4, R7).
- Prevention beats resolution: the radar exists to reduce what B5 has to merge.

### B5. Continuous merge preview

A background **scratch universe** perpetually merges all live forks (class-directed strategies, Doc 1 §8.3) and runs the project check.

- At any moment, `agit status` answers: `merge: clean ✓ check: passing ✓ (as of seal 9f3c, 40s ago)` — or names the conflicting pair and file.
- When agents finish, the final `agit merge` is a promotion of already-computed state: merge latency ≈ 0.
- Incrementality: only files changed since the last preview re-merge; checks re-run only when the merged state's snapshot ID is new (whole-state memoization, Doc 2 §2.5). Identical states are lookups.
- Resource honesty: preview runs at background priority, pauses on battery/thermal (Doc 1 §10.3 signals), and its check command execution is budgeted (`.agitpolicy: preview.check = on-idle | on-seal | off`). Default: merge computation always (cheap), check execution on-idle.
- **Not a command.** Preview state is a field in `status`/`forks` and an MCP resource. `agit merge --preview` prints the full detail on demand.

### B6. Speculative answers (the general principle)

Because states are content-addressed and results memoizable, idle cycles precompute the questions the developer is about to ask: *does it build, does it pass, does it merge.* B5 is one instance; per-seal speculative check runs (local or offloaded via Doc 2 runners) are the same machinery with a different trigger. Policy-gated, budget-capped, and silent — the user experiences it only as "answers are always already there."

Design rule for all of Set B: **no new verbs.** These capabilities are *qualities of existing surfaces* (`status`, `forks`, `merge`, MCP), not features to invoke.

---

## 4. Capability Set C — Capture Costs Nothing, So Time Gets Finer

### C7. Zero-latency seal (Tier-0 filesystems)

On Btrfs/ZFS, the atomic capture instant is a filesystem snapshot (~ms); hashing, chunking, and DAG construction proceed **lazily from the snapshot** in the background. Consequences:

- Seal latency at the moment of capture drops to ~0 regardless of delta size; Doc 1's completeness gate (Invariant 2) is satisfied when background ingest of that snapshot finishes — the snapshot ID is minted then, but the *instant* is preserved exactly.
- Sealing becomes affordable at **every agent tool call**, not just turn boundaries: the timeline becomes a flight recorder ("rewind to just before tool call #47").
- Database capture L2 (Doc 1 §9.4) comes free on the same tier.
- Non-Tier-0 platforms keep quiescence sealing unchanged; per-tool-call granularity is a Tier-0 privilege, disclosed.
- Retention interaction: per-tool-call seals are class `turbulent`-adjacent micro-points; they thin aggressively (minutes-scale) under Doc 1 §10.2 so storage does not balloon.

### C8. eBPF-assisted delta chunking (Linux, opt-in accelerator)

An eBPF write-syscall tracer records changed byte ranges per file, giving true O(delta) *reads* for large modified files — the cost we honestly disclaimed in Doc 1 §8.1 becomes recoverable where the OS allows.

- Strictly an **advisory accelerator** under Doc 1 Invariant 4: ranges hint the chunker where to start; the Merkle diff remains ground truth; tracer absence or overflow degrades to the standard full-read path with zero correctness impact.
- Requires CAP_BPF or equivalent; auto-enabled when available, visible in `agit status`, never required.

---

## 5. Capability Set D — Distributed Universes (RECORDED, DEFERRED)

Written down now; built later, properly, on Doc 2's Phase-4 transport. The strategic reason to defer: D's value depends on sub-second seal streaming, session keys, and runner economics being real — half-shipping distributed semantics would burn the "it just works" credibility A–C establish.

- **D9. One fork table, any location.** A universe on a Hetzner box or an AgentField subharness node appears in `agit forks` exactly like a local one — same diff, same radar, same merge preview, same claims. "Distributed" is a column, not a mode. Requires: coord-class eager replication across machines, seal streaming (target: remote activity visible locally within seconds), signed runner seals (Doc 2 §2.3).
- **D10. `agit exec -n 5 --cloud -- <cmd>`.** The capstone: five universes — three local, two remote — side-by-side output, radar between all of them, merge preview at the bottom, gate-verified winner at the end. This is the demo that defines the category, and it must not be attempted until A–C make the local version boring.
- Design constraint to honor now so D stays cheap later: **nothing in A–C may assume a fork is local.** Fork identity, radar input (dirty-path sets), and preview inputs (seals) must flow through the same DAG/journal abstractions whether the universe is a namespace on this machine or a mount on a runner.

---

## 6. CLI and Agent-Experience Design (anti-bloat contract)

### 6.1 Who is the caller?

Two callers, two disciplines:

- **Agents** call agit constantly → served **CLI-first in machine mode** (§6.3). Rationale: a CLI costs zero context until used and is discovered via `--help` (agents are shell-native); it needs no per-harness registration (on `$PATH` = integrated everywhere, the strongest form of vendor neutrality); it has no handshake latency; and it composes (`--json | jq`). MCP remains available as a **thin adapter generated from the CLI command registry** for harnesses that cannot shell out or prefer typed discovery — parity by construction, not by promise.
- **Humans** call agit occasionally → served by a **small porcelain** of memorable verbs whose *output* got richer (radar, preview, ports) instead of the verb list growing.

### 6.2 The porcelain (complete human surface — 12 verbs, hard budget)

```text
watch · snap · timeline · rewind · exec · forks · merge · try · shrink · status · sync · forget
```

Rules:
- **New capabilities land as columns/fields, not commands.** Radar → `forks`. Merge preview → `status` / `merge --preview`. Ports → `forks`. Seal internals (C7/C8) → invisible. Grades, fidelity, costs → `status`.
- `exec` subsumes routine `fork` usage; `fork`/`pair`/`claim`/`watch-fork`/`gc`/`remote`/`pin` are **plumbing** — present, documented, absent from the front page of `--help`.
- Adding a porcelain verb requires removing or demoting one. The budget is the feature.

### 6.3 Machine mode (the agent contract)

- Global `--json` on every command; `AGIT_AGENT=1` (or non-TTY detection) disables prompts, confirmations-by-question, colors, and relative-time parsing.
- Every mutation returns the IDs it created (`snapshot_id`, `fork_id`) — agents chain by ID, never by name guessing.
- Destructive operations follow Doc 1 §9.3: explicit ID + `--yes`; a prompt in machine mode is an error, not a hang.
- Stable, documented exit codes; errors are structured (`code`, `message`, `remedy`).
- **Events are CLI-native:** `agit events --follow` streams NDJSON (radar conflicts, preview transitions, seal/merge completions, remote-fork activity later). Agents background or poll it; hooks and MCP notifications are mirrors of this one stream, never separate sources.
- **`--help` is part of the agent contract:** terse, example-led, stable wording — it is the agent's discovery mechanism and is tested like an API (snapshot-tested output, budgeted length).
- **MCP is a generated adapter:** tools and read resources (timeline, fork table, preview state, events) are emitted from the same command registry the CLI is built from. Neither surface can have private capabilities, structurally.

### 6.4 What we refuse to build

Interactive wizards, config subcommand trees (`.agitpolicy` is the config surface), alias systems, plugins-in-core, per-framework helpers (fork hooks cover it), and any command whose output a column in `forks`/`status` could carry.

---

## 7. The One UI: Mission Control

**Yes to exactly one UI** — a local, daemon-served web page (`agit ui`, localhost) — and no others. Resolves Doc 1 open question 4 in favor of web over TUI.

Why a UI at all (three things terminals do badly):
1. **Timelines are spatial.** Scrubbing a flight-recorder history with per-tool-call seals (C7) wants a slider and sparklines, not paginated text.
2. **Triaging N universes is a review workload.** Side-by-side rich diffs of sibling forks with keep/discard/merge actions is the daily loop of parallel-agent work — the one place where visual bandwidth genuinely beats text.
3. **Preview wants glanceability.** The B5 green light (`merges clean · checks pass`) earns a persistent ambient surface; a browser tab or menu-bar dot is that surface.

What it is: read-mostly — timeline scrubber, fork table with radar and ports, live preview state, diff viewer — plus exactly the porcelain actions (rewind with impact preview, merge, keep/discard, pin). Served locally, zero accounts, works offline; screenshots of it are the product's organic marketing.

What it is not: an editor, an IDE, a settings panel, a chat surface, or a hosted dashboard (a phone-approval surface may arrive with Doc 2's hosted tier — out of scope here).

Anti-bloat guarantee: the UI renders the same JSON the CLI's `--json` emits — one data contract, two renderers. If the UI needs data the CLI can't produce, the CLI gains it first.

---

## 8. Delivery Mapping (against Doc 1 §16 — no Phase-0/1 footprint)

| Doc 1 phase | This document adds |
|---|---|
| Phase 0–1 | Nothing. Engine scope untouched. |
| Phase 2 (fork, MCP, follow-only sync) | **A1 `exec`** (mount-ns on Linux, sibling-dir fallback), **A2 v0** (netns ports Linux), **B4 radar** (dirty-set intersection + `events` stream), machine-mode contract incl. `events --follow` and generated MCP adapter (§6.3), **A3 benchmark** published with tier matrix |
| Phase 3 (merge) | **B5 continuous merge preview** (scratch universe, incremental re-merge, memoized checks), `merge --preview`, **Mission Control v1** (timeline, fork table, diffs, preview light) |
| Phase 4 (full sync, teleport, hosted) | **B6 speculative offload** (runner-backed checks), **C8 eBPF accelerator**, groundwork validation for D (fork-locality abstraction audit) |
| Phase 5 | **D9/D10 distributed universes** — gated on Doc 2 Phase-4 exit criteria |
| Tier-0 platforms, any phase ≥ 2 | **C7 zero-latency seal** + per-tool-call granularity (independent track; lands when Tier-0 fork backend lands) |

---

## 9. Risks

| # | Risk | Resolution | Residual |
|---|---|---|---|
| R1 | Unprivileged userns unavailable (hardened kernels, some distros) | Detect; `exec` falls back to sibling-dir mode with `$AGIT_WORKDIR`; disclosed via `exec --plan` | Same-path magic is Linux-mainstream, not universal |
| R2 | macOS lacks mount/net namespaces | Sibling dirs + env/proxy fallback, honestly disclosed; macOS keeps full A3/B/C value | A1/A2 are Linux-first; do not market them as universal |
| R3 | Preview check-execution burns CPU/battery | Background priority, on-idle default, thermal/battery pause (Doc 1 §10.3), policy-gated | Preview freshness lags on battery; shown as "as of" age |
| R4 | Per-tool-call seals balloon storage | Micro-seals thin at minutes-scale retention; C7 requires Tier-0 where snapshots are ~free; grades visible | Fine-grained rewind window is hours, not months |
| R5 | eBPF tracer gaps (dropped events, missing caps) | Advisory-only by construction (Doc 1 Invariant 4); silent fallback to full-read | None for correctness |
| R6 | CLI/MCP drift as features accrue | MCP is generated from the CLI command registry (§6.3) — drift is structurally impossible; porcelain budget is hard (§6.2) | Generator quality becomes the single point of care |
| R7 | UI scope creep | UI renders CLI JSON only; feature additions must land in CLI first | Perennial; the data-contract rule is the fence |

---

## 10. Open Questions

1. `exec` default when `-n > 1` and no check command exists: still run preview merge without gating, or nag for a check (relates Doc 1 OQ3)?
2. Radar sensitivity: path-level vs hunk-level overlap detection (hunk-level needs content diffing in the hot path — likely v2).
3. Mission Control packaging: bundled in the single binary (adds size) vs separate `agit-ui` artifact fetched on first `agit ui`.
4. Per-tool-call seal labels: adopt a generic hook naming convention now so any harness's tool names render in the timeline without agit knowing the harness (candidate: `AGIT_LABEL` env read at forced-boundary seals).
5. Whether `try` should be presented as porcelain sugar for `exec --once + diff + keep/discard` internally (one code path, two verbs) — leaning yes.
