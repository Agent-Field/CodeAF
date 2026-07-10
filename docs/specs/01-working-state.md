# furrow: The Working-State Layer for Every Repository

**Status:** Product and system specification, draft 0.3 (supersedes 0.2; original 0.1 was "Distributed Agent Workspace")
**Audience:** Product, architecture, security, and independent technical reviewers
**Scope:** A local-first engine that versions, forks, merges, and transports the *working state* of any repository — the layer git deliberately ignores
**Last updated:** 2026-07-09
**Companions:** [Document 2](02-distributed-runtime.md) covers the distributed runtime — ephemeral runners, network architecture, coordination primitives, DX tiers, and the open-source/commercial model. [Document 3](03-parallel-universes.md) covers the primary product theme (worktree replacement for parallel agents), capability sets A–C (true universes, precomputed convergence, zero-cost capture), the CLI/agent-experience anti-bloat contract, and the single sanctioned UI; it also resolves open question 4 (web UI). This document remains authoritative for engine semantics wherever they touch. Documents 2 and 3 have no Phase-0/1 footprint.

Draft 0.3 resolves the blocking review findings against 0.2: store placement, snapshot completeness, pool provenance, re-chunk cost claims, database capture semantics, fork-latency scaling, capture fidelity definition, rewind concurrency, remote chunk identity, and daemon budget reference configuration. It also adds first-class multi-machine sync to the open-source core.

---

## 1. Executive Summary

> **Git versions your commits. furrow versions everything between them.**

A developer's real working state is larger than what git tracks: dirty edits, untracked files, `.env`, the dev SQLite database, `node_modules`, build caches, generated files, and the `.git` directory's own mutable state (index, stashes, refs). Coding agents mutate all of it, at machine speed, in parallel. Git protects none of it.

furrow is a background engine plus CLI that gives any repository, in any language, four daily-use capabilities:

1. **Rewind** — continuous, near-free snapshots of the captured working state (fidelity defined precisely in §6). Scrub back to any moment, including untracked files, ignored files, and git's own state.
2. **Fork** — cheap copies of the full working state, dependencies and caches included, via tiered filesystem CoW. Run N agents in N parallel universes of one repo, then converge them with op-aware, verification-gated merge.
3. **Sync** — the same workspace on multiple machines, open source and self-hostable: daemon-to-daemon over SSH or through any dumb encrypted remote the user owns. Divergence between machines resolves through the same merge engine as agent forks.
4. **Teleport** — move the exact working state to another machine or cloud sandbox fast, with public dependency bytes hydrated from a provenance-verified registry chunk pool instead of the user's uplink.

furrow composes with git rather than replacing it: git history remains canonical for commits; furrow owns the uncommitted, ephemeral, parallel layer. It is language-agnostic by construction — the core operates on bytes, files, and file operations, never on parsed source.

The open-source core (engine, rewind, fork, merge, self-hosted multi-machine sync) is the adoption engine. The hosted plane (zero-config relay, registry pool CDN, managed encrypted backup, cloud-sandbox warm start) is the business: **capability is free, convenience is paid.**

---

## 2. Problem Statement

Three failures happen to working developers — especially those running coding agents — on a daily-to-weekly cadence:

1. **Unrecoverable working-state damage.** An agent (or its scripts, or the developer) corrupts or deletes state that git does not protect: untracked files, `.env`, a dev database, generated assets, an in-progress rebase. `git stash`, `reflog`, and editor undo each cover a fragment; nothing covers the workspace.
2. **Parallelism is expensive.** Running multiple agents on one repo requires isolation. `git worktree` copies only tracked files, so each parallel instance pays full dependency installation and cold build caches. In practice, developers serialize their agents.
3. **Working state does not travel.** Commits move between machines; the dirty tree, untracked files, stashes, and caches do not. Every cloud-agent session and every laptop→desktop handoff starts from a sterile checkout and a cold cache.

These are the same underlying gap: **there is no versioned, forkable, transportable representation of the working state.**

### 2.1 Why existing tools do not cover this

- **git / worktrees:** tracked files only; snapshots are manual; forks are shallow (no deps/caches).
- **Jujutsu (jj):** auto-snapshots the working copy, but tracked files only; no ignored/untracked capture, no CoW dependency forks, no dirty-state transport. Closest philosophical neighbor; furrow composes with it as it composes with git.
- **Dura / git-branchless:** background commits of tracked files only.
- **Time Machine / restic / borg:** too coarse, not repo-aware, no fork, no merge, no agent integration.
- **Docker / devcontainer snapshots:** image-granularity, minutes not seconds, heavyweight per fork.
- **Syncthing / Dropbox-class sync:** no snapshots-as-values, no fork, no merge semantics; actively dangerous on repos (sync `.git` mid-operation).

---

## 3. Product Surface

### 3.1 Commands (the daily loop)

```text
furrow watch                       # attach the engine to a repo (one-time; auto via agent hooks)
furrow snap -m <label>             # force a labeled seal right now
furrow timeline                    # browse snapshots: quiescent points, agent turns, labels, grades
furrow rewind [<time>|<snap>] [--paths <glob>...] [--dry-run]
                                 # preview → confirm → restore; leased, abortable, never destructive
furrow fork [<name>]               # full-state fork via best available CoW tier (cost disclosed first)
furrow forks                       # list universes, divergence summary, true disk cost
furrow merge <fork-or-machine> [--check "<cmd>"]
                                 # op-aware merge; gated on the check command passing
furrow pair <machine|remote-url>   # link another machine or register a self-hosted sync remote
furrow sync [--follow]             # push/pull snapshots between paired machines or via a remote
furrow teleport <target>           # exact working state → machine / cloud sandbox
furrow status [--fidelity]         # storage by class, engine health, capture-fidelity report
furrow forget [--purge]            # detach a repo; --purge deletes its store data
furrow gc                          # enforce retention and disk budget (also automatic)
```

### 3.2 Agent integration (the distribution channel)

- **MCP server** (`furrow mcp`): exposes snapshot/rewind/fork/merge/timeline as tools, so Claude Code, Cursor, and custom agents adopt furrow with one config line.
- **Turn-boundary hooks:** pre/post tool-use and turn-end hooks seal labeled snapshots ("before agent edit", "turn 14 complete"). The timeline reads as an agent flight path.
- **Auto-fork mode:** a hook policy that gives each spawned agent its own fork automatically and queues merges at turn end.

### 3.3 What the user must never need to understand

Chunks, Merkle trees, packs, quiescence sealing, or CoW tiers. The visible model is five nouns — **timeline, snapshot, fork, machine, teleport** — and `furrow status` explains cost, health, and fidelity in those terms.

---

## 4. Foundational Invariants

1. **Snapshots are immutable values.** Any change creates new objects and a new snapshot ID.
2. **A snapshot ID exists only when complete.** Every byte a snapshot references is durable in the local store before the ID is minted. There are no provisional or partially-backed snapshots; during initial ingest the timeline is simply empty and says so.
3. **Rewind is never destructive.** Every rewind first seals the current state; rewind is itself rewindable. No operation in the product discards unsaved state.
4. **The Merkle DAG is the only source of truth.** The operation journal, indexes, and caches are advisory accelerations; deleting them loses no data and changes no semantics.
5. **Snapshots are honest about consistency.** Each snapshot is labeled `quiescent` or `turbulent`; database captures carry their consistency contract (§9.4). The UI steers rewind and merge toward quiescent points.
6. **Capture fidelity is declared, not implied.** "Working state" means exactly the per-OS fidelity matrix (§6); anything not captured is enumerable via `furrow status --fidelity`, never silently dropped.
7. **The store never lives inside a watched root.** Packs, journals, and indexes reside in per-user application data; the in-repo `.furrow/` holds only configuration, policy, and hooks.
8. **Plaintext identity never leaves the machine unverified.** Remote storage uses opaque per-account chunk IDs; the only plaintext-hash disclosure is pool-eligible content that has passed registry provenance verification (§8.4), and it is disclosed as package identity, not raw content hashes to test.
9. **Resource use is bounded against a declared reference configuration** (§12). No operation allocates memory proportional to file size; ingestion and transport are streaming.
10. **furrow never mutates git's object store or history.** It captures `.git` as data but writes into it only during an explicit, leased, user-invoked rewind.
11. **Forks and syncs are cheap or honest.** Cost scales are disclosed before the operation runs (tier, entry count, bytes); degradation is reported, never silent.
12. **Merges are gated, attributed, and reversible.** No auto-merge lands without its verification check passing; every merge is a multi-parent snapshot; a bad merge is one rewind away.
13. **Local and self-hosted features work offline, forever, for free.** The hosted plane adds convenience and durability, never capability or correctness.

---

## 5. Canonical Data Model

Object identity is BLAKE3. All objects live in a per-user content-addressed store (CAS).

### 5.1 Store placement

- **Store root:** platform application-data directory — `~/Library/Application Support/furrow/store` (macOS), `${XDG_DATA_HOME:-~/.local/share}/furrow/store` (Linux), `%LOCALAPPDATA%\furrow\store` (Windows). Never inside any watched root; the watcher additionally hard-excludes any configured store path as defense in depth.
- **In-repo `.furrow/`:** configuration, `.furrowpolicy`, hooks, and a workspace-ID pointer only. Small, plain text, and itself captured (it is legitimate working state).
- **Fork materializations:** sibling directories (default `<repo>.forks/<name>`, configurable), each a watched root sharing the same store.
- **Lifecycle:** `furrow forget` detaches a repo; `--purge` removes its snapshots and any chunks unreachable from other workspaces. There is no "delete a directory inside the repo" footgun.

### 5.2 Chunk

Bounded byte sequence from content-defined chunking (FastCDC); the unit of dedup, storage, and transport.

```text
Chunk { id: blake3(plaintext), length, class_hint }
```

Class-tuned targets: whole-file for files < 64 KiB; ~64 KiB average for mutable text; 256 KiB–1 MiB for dependency/binary content. Chunking parameters are versioned; existing chunks are never reinterpreted after a parameter change.

**Remote identity is separate from local identity** (Invariant 8): a chunk stored on any remote — hosted or self-hosted — is addressed by `remote_id = HMAC(account_key, chunk_id)` with a ciphertext checksum for transport integrity. The remote can deduplicate within the account but cannot test membership of known plaintext. Pool chunks (§8.4) are the sole, provenance-gated exception and are addressed by public identity because their content is public.

### 5.3 Blob

```text
Blob { id, chunks[], total_length }
```

Streaming only; no implementation may require a full blob in memory.

### 5.4 Tree

Immutable directory node with sorted entries:

```text
TreeEntry {
  name
  kind: file | dir | symlink | fifo | socket-marker
  target_id
  mode, mtime_hint, size
  class
  link_group?        # hard-link group identity (§6)
  xattrs_id?         # blob of serialized xattrs incl. macOS resource forks
  acl_id?            # best-effort serialized ACLs
  sparse_map?        # hole extents for sparse files
}
```

Trees form a Merkle DAG with structural sharing: sealing re-hashes only dirty directories, so tree construction is O(changed entries × depth), never O(repo). (Byte-level capture cost is stated honestly in §8.1.)

### 5.5 Snapshot

```text
Snapshot {
  id
  root_tree_id
  parent_ids[]              # 2+ parents after merge (fork or machine convergence)
  sealed_at
  seal_quality: quiescent | turbulent
  trigger: interval | turn_boundary | pre_rewind | pre_merge | pre_sync | manual | storm_flush
  label?                    # "turn 14", "before npm upgrade"
  actor?                    # human | agent-id | machine-id (timeline UI metadata)
  machine_id                # which paired machine sealed it
  op_epoch                  # advisory journal position
}
```

### 5.6 Fork

```text
Fork {
  id, name
  base_snapshot_id
  materialization: fs-snapshot | overlay | per-file-clone | copy   # tier actually used (§8.2)
  head_snapshot_id          # forks have their own timelines
  created_at
}
```

### 5.7 Workspace and machines

```text
Workspace { id, roots[], policy, machines[], lease_state? }
Machine   { id, name, device_pubkey, transports[] }
```

A workspace synced across machines is one DAG namespace; each machine seals to its own head. Heads relate exactly like fork timelines (§8.5).

### 5.8 Operation journal (advisory)

```text
Op { epoch, kind: write|create|delete|rename|chmod, path, prior_file_id?, new_file_id?, confidence }
```

Inferred from watcher events plus file-ID tracking (inode/dev on POSIX, FileID on Windows) and content-hash matching. **Consumed by:** merge classification, timeline narration, re-chunk anchor hints. **Never consumed by:** snapshot correctness (Invariant 4). Event overflow marks an epoch dirty and triggers a subtree rescan.

### 5.9 Content classes

Class drives cadence, retention, transport, and merge behavior. Rule-based (defaults + `.furrowpolicy`), cheap, visible in `furrow status`:

```text
source        tracked / human-authored          full retention, merge=3way
vcs-meta      .git, .jj                         full retention, lock-aware capture, merge=never
config-secret .env, *.pem, key material         full retention, E2E-only transport, never pool-matched
dependency    node_modules, venv, vendor        manifest retained; bytes short-retention; pool candidate (provenance-gated, §8.4)
build-output  dist, target, .next, *.o          manifest retained; bytes short-retention; merge=regenerate
database      *.sqlite(+wal/shm), *.db          contract-labeled capture (§9.4); merge=never (prompt)
lockfile      package-lock, Cargo.lock          full retention; merge=re-resolve; provenance input for the pool
scratch       tmp, logs, caches, editor temps   minimal retention; merge=ours
```

Classification is a *local policy* mechanism. It never confers pool eligibility by itself (§8.4).

---

## 6. Capture Fidelity Matrix

"Entire working state" means precisely this, per OS. Anything marked ✗ is reported by `furrow status --fidelity` and documented; nothing is silently dropped.

| Aspect | macOS | Linux | Windows (staged, Phase 5) |
|---|---|---|---|
| Regular files, directories, symlinks, exec bits, mtimes | ✔ | ✔ | ✔ |
| Hard links | ✔ link groups detected (nlink/inode) and restored as links | ✔ | ✗ initially (restored as independent copies, reported) |
| Extended attributes | ✔ incl. resource forks / Finder info (as `com.apple.*` xattrs) | ✔ `user.*`; `security.*`/`system.*` best-effort | partial (staged) |
| ACLs | best-effort capture and restore, divergence reported | best-effort (POSIX ACL xattrs) | ✗ initially |
| Sparse files | ✔ holes preserved (`SEEK_HOLE`/`SEEK_DATA`) | ✔ | ✗ initially (materialized dense, reported) |
| NTFS alternate data streams | n/a | n/a | ✗ initially, documented |
| FIFOs | entry recreated (no contents — none exist) | ✔ | n/a |
| Sockets | recorded as marker, not recreated (runtime artifacts) | same | n/a |
| Device nodes | not captured (out of scope for repos), reported if present | same | n/a |
| Ownership (uid/gid) | recorded; restored only when restoring as the same user | same | — |
| Case/Unicode name forms | preserved byte-exact; cross-OS teleport collisions detected and reported before materialization | same | same |
| Git submodules | ✔ (directories like any other, including their `.git`) | ✔ | ✔ |
| Git worktrees / alternates | captured within the watched root; object stores *outside* the root are out of scope — furrow detects alternates/worktree pointers that leave the root and warns explicitly | same | same |
| Running processes, ports, containers | ✗ by design (files, not processes; see fork hooks §8.2) | ✗ | ✗ |

Cross-OS teleport applies a declared normalization step (name-form collisions, mode bits, link groups) and refuses with a precise report rather than guessing.

---

## 7. Architecture

```text
                       ┌────────────────────────────────────────────┐
                       │              furrow daemon (per user)         │
  FS events ──────────►│ watcher → classifier → dirty-set tracker    │
  (FSEvents/fanotify/  │      │                     │                │
   inotify/RDCW +      │  op-journal (advisory)     ▼                │
   rescan fallback)    │                     quiescence sealer       │
                       │                            │                │
                       │        chunker (FastCDC, anchor reuse)      │
                       │                            │                │
                       │  per-user CAS (app-data): append-only packs │
                       │   + journal · Merkle DAG · retention/GC     │
                       ├────────────────────────────────────────────┤
                       │ CoW fork engine (tiered backends)           │
                       │ merge engine (classify → 3way/op → gate)    │
                       │ sync engine (machines, leases, remotes)     │
                       │ transport (teleport, pool client)           │
                       │ MCP server · CLI RPC · agent hooks          │
                       └───────────┬───────────────────┬────────────┘
                     E2E-encrypted │                    │ E2E-encrypted
                      ┌────────────┴───────┐   ┌────────┴───────────┐
                      │ self-hosted (OSS)  │   │ hosted plane (paid) │
                      │ peer daemon (SSH), │   │ relay, registry     │
                      │ S3/WebDAV/sftp     │   │ chunk pool CDN,     │
                      │ dumb remotes       │   │ managed backup,     │
                      └────────────────────┘   │ sandbox warm-start  │
                                               └────────────────────┘
```

One daemon per user serves all watched repos and hosts the shared CAS; `dependency`-class chunks dedupe across every repo on the machine.

---

## 8. Technical IP

Five pillars. Each states its target, mechanism, and honest cost model.

### 8.1 IP-1: Sub-second whole-workspace snapshot engine

**Target:** sealing a snapshot costs **O(changed files' bytes) read + O(delta) new storage**, completing in p95 ≤ 1 s for typical edit deltas (≤ 100 changed files, ≤ 64 MiB changed bytes) on a 1M-file repo, at < 1% idle CPU.

**Cost model, stated honestly:** filesystem watchers identify changed *paths*, not changed byte ranges. Correctness therefore requires reading each modified file in full to locate and verify changes; there is no general O(delta-bytes) read without write interception or filesystem changed-extents support (neither assumed). What *is* O(delta): new chunk storage, tree re-derivation (dirty directories only), and dedup work. Anchor-based boundary reuse (previous chunk boundaries as anchors, resynchronizing by the content-defined property) minimizes new-chunk churn for large edited files — it saves storage and downstream transport, not reads. Where a platform later exposes reliable changed-extent info, it slots in as an optimization, never a correctness dependency.

**Mechanism:**

- **Dirty-set tracking, not scanning.** The watcher maintains a dirty directory set; sealing touches only dirty entries and the tree spine above them. Periodic low-priority verification scans (and mandatory rescans after event overflow) bound dropped-event damage.
- **Quiescence sealing.** A snapshot seals after an adaptive write-silence window (default 500 ms) or immediately at forced boundaries (agent turn end, pre-rewind, pre-merge, pre-sync). During write storms (`npm install`, builds) the sealer backs off, emits at most sparse `turbulent` checkpoints, and seals a `quiescent` snapshot when the storm ends — the pathological load case becomes the cheap case.
- **Torn-write defense.** Per-file capture rechecks size/mtime/file-ID after read and retries on change; coupled files (database groups, `.git` internals) are captured as **verified-stable groups**: every member must be individually unchanged across the whole group read window, else the group retries at next quiescence. (This bounds, but does not prove, simultaneity — the honest contract is in §9.4.)
- **Completeness gate (Invariant 2).** A seal commits only after all referenced chunks are durable in the CAS. Initial attach runs prioritized ingest — `config-secret`, `source`, `vcs-meta` first (small, seconds), `dependency` bytes last (often already present in the global store from sibling repos). The UI is live during ingest (progress, ETA, browsing); *protection begins at the first complete seal*, and the timeline says exactly when that is. No snapshot ever references bytes the store does not hold.

### 8.2 IP-2: Tiered fork engine

**Target:** fork cost that is O(1) in entry count where the platform allows, O(entries) metadata where it doesn't — **with the tier, projected latency, and disk cost disclosed before the fork runs** (Invariant 11).

**Tiers, ordered by preference; furrow selects the best correct backend per filesystem:**

- **Tier 0 — native filesystem snapshots (O(1) in entries):** Btrfs/ZFS subvolume or dataset snapshot+clone when the repo resides on one. Constant-time regardless of tree size; the headline `< 1 s for any repo` claim applies **only here**.
- **Tier 1 — overlay mounts (O(1) in entries):** overlayfs in an unprivileged user namespace (Linux); lower = sealed snapshot materialization, upper = divergence. Near-constant-time; per-kernel syscall caveats documented.
- **Tier 2 — per-file CoW clone (O(entries) metadata, zero data copy):** APFS `clonefile`, XFS/Btrfs reflink, ReFS block clone. Realistic throughput is on the order of 10⁴–10⁵ entries/s, so: ≤ 100k entries ≈ ≤ 1 s; a 1M-entry tree is **tens of seconds** and furrow says so before starting. This is the best available tier on macOS (APFS snapshots are volume-level and unsuitable for per-repo forks).
- **Tier 3 — deduped eager copy (fallback, incl. NTFS):** parallel copy; store-level dedup intact; working-copy bytes real. Cost projected up front; Dev Drive/filesystem migration recommended where relevant.

Fork hooks (`.furrow/hooks/fork`) remap ports, database names, or `.env` values per universe — a fork copies files, not running processes (§6 last row; risk R14).

### 8.3 IP-3: Op-aware, verification-gated merge

**Target:** N divergent timelines — agent forks or machines — converge with near-zero human conflict labor, without ever landing an unverified result.

**Mechanism:**

- **Exact merge bases for everything.** Every merge has a true three-way base covering untracked and ignored files — a base git structurally cannot provide. Rename/move handling uses op-journal file-ID lineage instead of similarity guessing.
- **Class-directed strategies:** `build-output` → regenerate, don't merge; `lockfile` → re-run the resolver; `database`/binary → ours/theirs prompt; `scratch` → ours; `source` → three-way, with conflicts escalated to an LLM resolver whose input is both op-sequences plus base/side contents — materially richer than a diff3 hunk.
- **The gate:** merged state materializes into a scratch fork and must pass the project's check command before the merge snapshot is published. Failures land as a conflict workspace, never silent breakage.
- **Safety inversion:** continuous snapshotting is what makes aggressive merge automation safe — a wrong merge is one rewind away.

### 8.4 IP-4: State teleport with a provenance-verified registry chunk pool

**Target:** first-time transport of a workspace with multi-GB dependencies is bounded by *divergence from verified public content*, not workspace size; repeat teleports ship deltas; cloud sandboxes warm-start in seconds.

**Pool eligibility is provenance, never path classification.** `node_modules` may contain private-registry packages, `file:` dependencies, patched files, post-install output, and credentials — none of which are public. The gate:

1. **Lockfile provenance:** the client parses lockfiles (`package-lock.json` integrity hashes, pnpm/yarn locks, `Cargo.lock` checksums, pip/poetry/uv hashes, `go.sum`) to map dependency directories to `(registry, package, version, integrity)` claims.
2. **Pool extraction manifests:** the hosted pool ingests **public registries only**, extracting each package tarball under deterministic, versioned extraction rules and publishing a manifest: the exact expected file set with per-file BLAKE3 for that `(package, version, integrity)`.
3. **Per-file verification:** a file is pool-eligible iff its lockfile entry's integrity matches a pool manifest **and** the file's own hash is in that manifest. Patched files, post-install artifacts, private packages, and anything else fails closed to the private path.
4. **Minimal disclosure:** because eligibility is decided client-side against the manifest, no membership query over raw content hashes ever occurs. Teleport discloses only "this account references public package P@V" — disclosed in docs, shown in `furrow status`, opt-out (falls back to full encrypted upload).

**Everything non-eligible is E2E encrypted** (XChaCha20-Poly1305, per-account keys, device keychain + recovery phrase) and addressed by opaque `remote_id` (§5.2) — the server, hosted or self-hosted, cannot test known-content membership. Convergent encryption within the account boundary only; cross-account plaintext dedup is rejected permanently.

**Teleport = manifest diff + missing chunks:** the receiver reports its have-set by manifest ancestry; pool-eligible chunks hydrate from CDN; the private remainder ships encrypted; interrupted transfers resume by chunk.

### 8.5 IP-5: Multi-machine sync (open source, self-hostable)

**Target:** an indie developer with a laptop, a desktop, and a home server keeps one workspace live across all of them — "work at the latest" — using only open-source furrow and infrastructure they already have.

**Mechanism:**

- **Pairing:** `furrow pair` exchanges device keys (SSH bootstrap or short code). A workspace's machines share one DAG namespace; each machine seals to its own head (§5.7).
- **Transports (both OSS):** direct daemon↔daemon over SSH (works on LAN, Tailscale, any reachable host), or a **dumb remote** the user owns — S3-compatible bucket, WebDAV, sftp directory — holding encrypted chunks and manifests. E2E encryption applies to self-hosted remotes too: the bucket sees ciphertext and opaque IDs only.
- **Follow mode:** `furrow sync --follow` pushes on seal and auto-pulls. If the local head is an ancestor of the incoming head → fast-forward materialization (leased like rewind, §9.3). If both machines diverged → the heads are siblings, resolved by the standard merge engine (`furrow merge machine/laptop`) or interactive pick. Divergence is never lost, only surfaced.
- **Single-writer lease (optional):** for strict "latest" semantics, a per-workspace lease is granted via the sync path — conditional-put on dumb remotes (e.g. S3 `If-None-Match`), pairwise grant over SSH. A stale lease from an offline machine can be taken over explicitly; the offline machine's later seals become siblings, not casualties.
- **What stays paid:** zero-config relay (no SSH/Tailscale setup), the registry pool CDN (self-hosted syncs move dependency bytes themselves or regenerate via install), managed durable backup, cloud-sandbox warm-start integration. The Syncthing lesson, applied deliberately: OSS multi-machine is the strongest proof of the local-first posture, and the subscription sells infrastructure, not capability.
- **Staged delivery — follow-only subset first (Phase 2):** multi-machine ships before the merge engine as a restricted mode: pairing, SSH/dumb-remote transport, and follow mode with the **single-writer lease mandatory**. Only fast-forwards execute; if a machine seals without holding the lease (e.g. edited while offline), its head is parked as a sibling — preserved, browsable, restorable by explicit pick (`furrow rewind`/materialize), but not merged. Full divergence convergence arrives with the merge engine (Phase 4 lifts the restriction). This gives "work at the latest across my machines" a full product phase earlier without ever risking silent state loss.

---

## 9. Core Workflows

### 9.1 Attach and first seal

1. `furrow watch` registers the repo; classifier applies defaults + `.furrowpolicy`; store lives in app-data (§5.1).
2. Prioritized ingest streams classes in order (`config-secret`/`source`/`vcs-meta` → `database`/`lockfile` → `dependency`/`build-output`), deduping against the global store.
3. **First complete seal** fires when all referenced bytes are durable (Invariant 2); the UI shows progress and the moment protection begins. Typical repos with a warm global store: seconds. Cold multi-GB dependency trees: minutes, with an ETA — never a snapshot that lies.
4. Engine drops to event-driven idle.

### 9.2 Continuous protection (steady state)

Watcher events → dirty set + op journal → quiescence sealer emits complete snapshots on silence and at agent-turn boundaries → retention thins history (§10.2). Idle repos cost zero snapshots.

### 9.3 Rewind (leased)

1. `furrow rewind 20m` (or snapshot ID / timeline pick, optionally `--paths`).
2. **Impact preview and confirmation.** Before anything runs, furrow shows a concise preview: target snapshot and its seal quality/grade, counts of files restored/deleted/changed by class, database captures involved and their contract level, and processes holding open write handles on affected paths. Interactive human invocations require confirmation (`--dry-run` shows the preview alone). **Noninteractive/agent invocations require an explicit snapshot ID plus `--yes`** — relative times and confirmations are for humans; agents must name exactly what they restore.
3. Engine seals current state (`pre_rewind`) — Invariant 3.
4. Engine takes the **workspace mutation lease**: agent hooks pause managed agents holding the workspace; open write handles and known dev-server processes touching target paths are detected and reported.
5. Restore executes as a monitored critical section. If concurrent writes to target paths are observed mid-restore, the default policy **aborts and rolls back to the pre-rewind seal**, naming the offending paths/processes. `--force` selects explicit best-effort mode instead. There is no "warn and hope" mode.
6. `vcs-meta` restore is additionally lock-aware: an in-progress git operation (`index.lock`, rebase/merge state) causes refusal of that subtree with a precise explanation.
7. Result: workspace, `.env`, dev DB, and git's own branch/index/stash state return to the chosen moment — within the declared fidelity matrix (§6).

### 9.4 Database capture contract (tiered, labeled)

A sequentially-read group cannot prove a single instant. furrow therefore offers three explicit levels, and every database capture is labeled with the level that produced it:

- **L0 (default) — best-effort crash-consistent:** verified-stable group capture (§8.1) of `db + WAL + SHM` at quiescence. For crash-safe engines (SQLite in WAL or rollback mode), a group whose members were individually stable across the read window is *very likely* equivalent to a valid crash image, but this is a best-effort contract, not a proof. Restores of known formats run an integrity probe and report.
- **L1 (auto-scheduled at boundaries) — engine-aware:** for SQLite, capture via the online backup API / `VACUUM INTO` from a read-only helper process — a consistent point-in-time image by the engine's own semantics, at the cost of touching the database through SQLite rather than the filesystem. **Scheduling:** SQLite is auto-detected; L1 runs at forced boundaries (manual seals, agent-turn seals, pre-rewind/pre-merge/pre-sync), while continuous background seals use labeled L0 to bound overhead. If L1 fails (locked/busy database), the seal falls back to L0 with the label saying so. Per-path policy can force L1-always, L0-only, or disable database capture.
- **L2 — filesystem-snapshot-backed:** where the repo sits on a Tier-0 filesystem (§8.2), the group is read from an atomic Btrfs/ZFS snapshot: true single-instant capture with no engine cooperation.

Non-crash-safe stores are documented as L0-only best-effort. `merge` never auto-merges `database` class regardless of level.

### 9.5 Fork / parallel agents

1. `furrow fork agent-a` → tier selected, projected latency and disk cost shown, then materialized; fork timeline begins.
2. Agent works in the fork; turn hooks seal labeled snapshots.
3. `furrow merge agent-a --check "npm test"` → class-directed merge → gate → two-parent snapshot, or a conflict workspace.

### 9.6 Multi-machine daily loop

1. `furrow pair desktop` once (or `furrow pair s3://my-bucket/furrow` for a dumb remote).
2. `furrow sync --follow` on each machine.
3. Laptop seals → pushes; desktop fast-forwards on next quiescence (leased materialization, §9.3 semantics).
4. Both edited while apart → siblings on reconnect → `furrow merge machine/laptop --check "npm test"` converges them like any agent fork.

### 9.7 Teleport

Manifest diff → provenance-verified pool hydration for eligible dependency chunks → encrypted upload of the private remainder → receiver seals an identical snapshot and materializes (fidelity normalization per §6 if cross-OS). Resumable by chunk; verified chunks never retransmit.

---

## 10. Storage, Retention, and Budgets

### 10.1 Local store

- Append-only pack files with per-chunk checksums; a snapshot record is journaled only after its chunks are durable (fsync ordering); recovery truncates torn pack tails and replays the journal. Crash at any instant loses at most the unsealed window.
- Per-user global CAS (all classes; `dependency` chunks dedupe across repos). Per-repo reachability metadata makes `furrow forget --purge` precise.
- Background scrub verifies pack checksums; corrupt chunks re-fetch from any replica (fork, machine, remote, pool) or mark affected snapshots degraded — never silently wrong.

### 10.2 Retention (two dimensions: manifests and bytes)

Retention is defined separately for **manifests** (snapshot structure, entries, hashes — cheap, kept long) and **bytes** (chunk contents — the disk cost). This is what reconciles a thinning timeline with honest reconstruction claims:

- **Materialization grade.** Every snapshot has a derivable grade, shown in `furrow timeline`:
  - `exact` — every referenced byte is present; byte-for-byte materialization guaranteed.
  - `partial(classes)` — manifests are exact for the whole tree, but byte retention has lapsed for the named classes; furrow lists every non-restorable path with its recovery route (pool provenance, regeneration command, or none).
- **Byte-drop rule.** A chunk's bytes are eligible for deletion only when unreachable from **all** of: (a) any snapshot inside its class's byte-retention window, (b) any fork or machine head, (c) any pin. Consequence: dependency bytes that haven't changed remain reachable from current heads and therefore stay `exact` indefinitely — only *orphaned* versions (replaced packages, stale build outputs) age out.
- **Snapshot thinning** (which snapshot IDs the timeline keeps) and **byte windows** (how long each class's bytes outlive thinning) default to:

```text
manifest retention (all classes):
    every snapshot 24 h → hourly 7 d → daily 90 d → weekly thereafter
byte retention:
    source / vcs-meta / config-secret / lockfile:  as long as any retained manifest references them
    database:                                      as long as any retained manifest ≤ 30 d references them
    dependency / build-output:                     72 h beyond last reachable reference from heads/pins
    scratch:                                       24 h total
```

- **Nothing degrades silently.** Before a drop takes a snapshot from `exact` to `partial`, `furrow status` reports it and states the recovery route; `pin` upgrades any snapshot to permanent `exact`.

A hard disk budget with a reserved-free-space floor overrides byte windows (oldest eligible bytes first; pinned, head-reachable, and in-window chunks protected). `furrow status` shows bytes by class and the current grade distribution of the timeline.

### 10.3 Client resource budgets (reference configuration)

Budgets are stated against a declared reference, not "unlimited repos":

- **Reference configuration:** 10 watched repos, 2 M total watched entries, default cadence.
- **Idle at reference:** < 1% CPU, ≤ 150 MiB daemon RSS, zero disk writes with no FS events. Manifests, dirty-set indexes, and journal state are disk-backed (mmap) — RSS does not scale with history size.
- **Beyond reference:** approximately +40 MiB RSS per additional 1 M watched entries; a configurable hard cap triggers **idle-repo hibernation** (watcher detached, epoch marked dirty, rescan-on-wake — correctness preserved by Invariant 4's rescan discipline).
- **Active sealing:** bounded worker pool; hashing/chunking concurrency capped; ≤ 128 MiB in-flight buffers; streaming everywhere (Invariant 9).
- Battery/thermal/interactive-load signals throttle sealing to forced-boundary-only mode; forced boundaries and explicit commands always work.

---

## 11. Risk Register and Resolutions

| # | Risk | Resolution | Residual |
|---|---|---|---|
| R1 | Event storms (installs/builds, 10⁴–10⁵ events/s) | Storm back-off → sparse `turbulent` checkpoints → `quiescent` seal after silence (§8.1); relaxed cadence classes | Rewind granularity coarsens during storms; labeled |
| R2 | Torn captures of half-written files | Recheck-and-retry; verified-stable groups; quiescence sealing; seal-quality labels | `turbulent` snapshots may hold mid-build states; UI steers away |
| R3 | Live databases captured mid-transaction | **Tiered, labeled contract (§9.4):** L0 best-effort crash-consistent (stated as such, not "valid by definition"), L1 SQLite backup API opt-in, L2 fs-snapshot true point-in-time; restore-time integrity probes | L0 is best-effort by name; non-crash-safe stores documented |
| R4 | FS event unreliability (drops, coalescing, overflow) | Journal advisory-only (Invariant 4); overflow → dirty-epoch rescan; periodic verification scans | Dropped events delay capture until rescan; cannot corrupt a seal |
| R5 | Store inside the watched root (recursive capture) | **Structurally impossible:** store in per-user app-data (§5.1, Invariant 7); watcher hard-excludes store paths; in-repo `.furrow/` is config only | None |
| R6 | Incomplete snapshots during background ingest | **Complete snapshots only (Invariant 2):** no ID until all bytes durable; prioritized ingest; UI-live-but-honest attach (§9.1) | Cold multi-GB attach delays first protection by minutes, with ETA |
| R7 | Fork latency overclaimed for large trees | Tier restructure (§8.2): O(1) claims restricted to fs-snapshot/overlay tiers; per-file clone stated O(entries) with throughput math; cost disclosed pre-fork | macOS (Tier 2 best) 1M-entry forks take tens of seconds — disclosed, not hidden |
| R8 | Re-chunk cost overclaimed as O(delta) | Honest cost model (§8.1): O(changed files' bytes) read; O(delta) applies to storage/trees/transport; anchors are an optimization, changed-extents info optional-only | Large modified files cost a full sequential read — physics |
| R9 | Private bytes leak via the pool (private registries, patches, postinstall output) | **Provenance gate (§8.4):** lockfile integrity → pool extraction manifest → per-file hash membership, verified client-side, fail-closed to encrypted path; classification never confers eligibility | Disclosure reduces to public package identity at teleport; opt-out exists |
| R10 | Remote tests known-content membership | Opaque `remote_id = HMAC(account_key, chunk_id)` + ciphertext checksums for all non-pool chunks, hosted **and** self-hosted (§5.2, Invariant 8) | Server learns sizes/timing of ciphertext only |
| R11 | Secrets leave the machine (`.env` is core content) | E2E by default for all transport; `config-secret` never pool-matched, never plaintext-identified remotely; local-only mode exists | Key loss = remote-backup loss; recovery phrase mandatory at onboarding |
| R12 | LLM merge lands wrong code | Verification gate in a scratch fork; conflicts land as workspaces; merges multi-parent and rewindable (§8.3) | Weak/absent check command weakens the gate; furrow nags per repo |
| R13 | Crash corrupts the CAS | Append-only packs, per-chunk checksums, journal-after-durable, torn-tail truncation, scrub (§10.1) | ≤ unsealed window lost |
| R14 | Fork ≠ running environment (ports, daemons) | Explicit scope (files, not processes; §6); fork hooks remap per universe | Environment virtualization is a non-goal |
| R15 | Rewind races concurrent writers | **Mutation lease (§9.3):** agent pause hooks, monitored critical section, abort-and-rollback default, explicit `--force` best-effort | Unmanaged external processes can force an abort; reported precisely |
| R16 | Capturing `.git` during active git operations | Lock/operation-state detection defers `vcs-meta` sealing and refuses mid-operation rewind | Mid-rebase seals mark `vcs-meta` turbulent |
| R17 | GC deletes chunks a fork or machine still needs | Mark-sweep roots include fork heads, machine heads, pins; grace period; refcounts never authoritative | Worst case delayed reclamation, never premature deletion |
| R18 | Fidelity gaps misrepresent "entire working state" | **Declared fidelity matrix (§6, Invariant 6);** `furrow status --fidelity`; cross-OS normalization refuses rather than guesses | Windows fidelity staged; ADS/sparse/ACL gaps explicit |
| R19 | Daemon budget unbounded across repos | Reference configuration + disk-backed indexes + idle-repo hibernation with rescan-on-wake (§10.3) | Beyond-reference RSS grows linearly and predictably |
| R20 | Multi-machine split-brain | Sibling-head model (never lost work), optional single-writer lease with explicit stale-lease takeover, merge-engine convergence (§8.5) | Simultaneous offline edits require a merge — surfaced, not auto-guessed |
| R21 | Battery/thermal drain | Event-driven (no polling), BLAKE3 SIMD, storm back-off, throttle-to-boundaries on battery | Heavy churn on battery coarsens cadence; visible |
| R22 | Editor atomic-save rename churn | File-ID lineage in op journal; content-keyed chunks; editor temp patterns classed `scratch` | None significant |
| R23 | Jujutsu or a major vendor drifts into the space | Speed to the composed whole; jj composes rather than competes today; pool + platform-CoW matrix + agent-hook channel are slow-to-copy | Real risk; answered by sequencing (§16), not architecture |
| R24 | Very large single files (model weights, media) | CDC + anchor reuse → O(delta) storage; streaming; class-tunable chunk sizes | Ingest/re-read bounded by sequential disk speed — physics |

---

## 12. Service Objectives and Engine Targets

Design targets to validate before external claims — measured on defined reference hardware and the §10.3 reference configuration.

| Metric | Target |
|---|---:|
| Seal latency: ≤ 100 changed files, ≤ 64 MiB changed bytes, 1M-file repo | p95 ≤ 1 s |
| Seal cost scaling | O(changed bytes) read; O(delta) storage; never O(repo) |
| Idle daemon CPU / RSS at reference config (10 repos / 2M entries) | < 1% / ≤ 150 MiB |
| Fork latency, Tier 0–1 (fs-snapshot / overlay), any entry count | p95 ≤ 1 s |
| Fork latency, Tier 2 (per-file clone) | p95 ≤ 1 s per 100k entries; projected and disclosed pre-fork |
| Rewind, ≤ 1k changed paths (incl. lease acquisition) | p95 ≤ 2 s |
| Merge (excluding check command runtime) | p95 ≤ 10 s |
| Machine sync propagation, warm, same LAN/tailnet | p95 ≤ 5 s after seal |
| Teleport, warm (delta only) | p95 ≤ 15 s |
| Teleport, cold, ≥ 80% provenance-verified pool hit on dependencies | bounded by private-divergence upload, not workspace size |
| Store overhead vs. logical unique data (median file ≥ 64 KiB) | ≤ 10% |
| Crash data loss | ≤ unsealed window (target ≤ 5 s of changes) |
| Hosted plane availability | 99.9% monthly (local and self-hosted features unaffected) |

---

## 13. Validation Plan

Adversarial datasets and scenarios, all automated:

1. 1M small files; monorepo with live `node_modules` churn; 100 GB single file with small mid-file edits.
2. `npm install` / `cargo build` storms during sealing — storm back-off, post-storm quiescent correctness, CPU ceiling.
3. Kill -9 the daemon at every phase (mid-pack, mid-journal, mid-seal, mid-rewind, mid-merge, mid-sync) — recover, verify every sealed snapshot byte-for-byte.
4. SQLite under sustained write load at L0/L1/L2 — restore, integrity-check, verify each level's labeled contract holds.
5. Event-loss injection (synthetic overflow) — rescan restores fidelity; no corrupt seals.
6. Fork tier matrix: Btrfs/ZFS, overlayfs, APFS/XFS clone, NTFS copy — latency vs. entry count matches §12 claims; disclosure accuracy.
7. 10 concurrent forks with renames, lockfile changes, generated output — merge all back, gated; convergence and disk ≈ divergence.
8. Rewind vs. live writers: dev server writing during restore → abort-and-rollback fires, offenders named; `--force` path verified; `vcs-meta` refusal during a staged rebase.
9. Pool provenance adversarial set: patched package, postinstall-generated files, private-registry package, `file:` dependency, tampered lockfile integrity — all must fail closed to the encrypted path.
10. Teleport at pool hit rates {0%, 50%, 95%}; interrupted/resumed transfers; corrupt-chunk injection from a hostile remote (reject and re-fetch); cross-OS fidelity normalization refusals.
11. Multi-machine: follow-mode fast-forward; simultaneous offline edits → sibling surfacing and merge; stale-lease takeover with the offline machine's later seals preserved as siblings; dumb-remote (S3/WebDAV) conditional-put lease correctness. **Follow-only subset (Phase 2 gate):** the same suite with merge disabled — divergence must park as pick-only siblings, never fast-forward over unmerged work, never lose a byte.
12. Fidelity matrix conformance per OS: hard-link groups, xattrs/resource forks, sparse holes, ACL best-effort reporting, alternates/worktree warnings.
13. Budget conformance: reference-config RSS/CPU; hibernation and rescan-on-wake beyond reference; battery throttle with forced boundaries still firing.
14. Six-month simulated timeline — retention thinning, GC with live forks and machine heads, disk budget enforcement, scrub-driven repair.

Acceptance gates: byte-for-byte reconstruction of every snapshot graded `exact` (i.e., within its byte-retention envelope, §10.2); for `partial` snapshots, manifest-exact enumeration of every non-restorable path with its recovery route — reconstruction claims never exceed the envelope, and no grade transition occurs unreported; zero incomplete snapshot IDs ever observable; deletion of all advisory structures followed by full recovery from the DAG; all §12 targets measured on reference hardware.

---

## 14. Security Model

- **Local-first trust boundary:** all core features function with zero network; the CAS inherits user file permissions.
- **Transport encryption:** client-side XChaCha20-Poly1305, per-account keys, device keychain + recovery phrase generated at signup (loss = remote-backup loss; stated at onboarding). Applies identically to hosted and self-hosted remotes.
- **Identity separation:** local plaintext BLAKE3 IDs never leave the machine; remotes see `HMAC(account_key, chunk_id)` + ciphertext checksums. No remote — hosted, S3, or WebDAV — can test known-plaintext membership.
- **Pool boundary:** plaintext identity is used remotely only for provenance-verified public registry content (§8.4), where the client has already proven the pool holds the bytes; the residual disclosure is public-package usage at teleport time — documented, visible, opt-out.
- **Secret hygiene:** detector (path patterns + entropy) feeds `config-secret`; the class is structurally excluded from pool matching and any plaintext-identity path; per-path exclusion from remote sync is available while keeping local protection.
- **No provider trust:** every chunk verifies against its identity on read, local or remote; substitution is detected, recorded, repaired from another replica.
- **Attack-surface honesty (ships in docs verbatim):** the hosted plane learns account identity, encrypted-chunk sizes and timing, and — unless opted out — public-package usage during teleport. It never learns file names, paths, source bytes, or secrets.

---

## 15. Non-Goals

- Replacing git, GitHub, code review, or CI. furrow composes with them.
- Process/environment virtualization (services, containers, port namespaces) — fork hooks are the extension point.
- Language-aware semantic indexing in the core; language semantics enter exactly once, optionally, at LLM merge resolution.
- Enterprise audit/provenance/compliance surfaces in v1 (derivable later from the DAG without re-architecture).
- Cross-account plaintext deduplication — rejected permanently.
- Guaranteed capture of state outside the declared fidelity matrix (§6).
- General O(delta-bytes) read cost without OS support — claimed nowhere (§8.1).

---

## 16. Delivery Phases

**Phase 0 — POC (subset of Phase 1).**
Boundary: `furrow watch`, `furrow snap -m <label>` (manual seal), `furrow timeline`, `furrow rewind` (including `--paths` and `--dry-run` — recovering a single `.env` is the hero scenario), `furrow status`, `furrow forget`. One repository at a time; macOS and Linux; SQLite adapter (auto-detect, L1 at forced boundaries, L0 continuous per §9.4). No fork, sync, MCP, or hosted components. **Default capture includes `node_modules`, build caches, and other large ignored directories** — this is the differentiation from git and must be proven at real-world scale — with an onboarding size estimate (post-dedup projected store cost) and one-line `.furrowpolicy` exclusions. Rewind UX per §9.3: impact preview, interactive confirmation, agent path gated on explicit snapshot ID + `--yes`. *Exit: attach → break the workspace (delete `.env`, corrupt a rebase, trash a dev DB) → preview → rewind → byte-exact recovery, on both OSes, with §10.3-order overhead.*

**Phase 1 — Engine + rewind (macOS, Linux).**
Watcher, classifier, quiescence sealer, app-data CAS/packs/journal, Merkle DAG, completeness gate, retention/GC, crash recovery, mutation lease, fidelity matrix v1, `watch/timeline/rewind/status/forget`. *Exit: kill-matrix and rewind-vs-writers tests green; a user recovers a deleted `.env` and a broken rebase.*

**Phase 2 — Fork + agent hooks + MCP + follow-only sync.**
Tier 0/1/2 backends with pre-fork disclosure, fork timelines and hooks, MCP server, turn-boundary integration, auto-fork policy. Plus the **follow-only multi-machine subset** (§8.5): pairing, SSH and dumb-remote transports, opaque remote identity and E2E encryption, follow mode with mandatory single-writer lease, fast-forward-only materialization, offline divergence parked as pick-only siblings. *Exit: 5 parallel agents on one laptop, zero interference, fork disk ≈ divergence; tier-latency matrix validated; laptop↔desktop follow loop green including lease takeover and sibling parking.*

**Phase 3 — Merge.**
Op lineage, class strategies, three-way-with-ops, LLM resolver, verification gate, conflict workspaces. *Exit: validation scenario 7 green.*

**Phase 4 — Full sync + teleport (OSS self-hosted and hosted plane together).**
Lifts the Phase-2 follow-only restriction: divergent machine heads converge through the merge engine; optional-lease mode becomes available. Adds teleport, E2E backup; hosted relay, provenance-verified pool (npm first, then PyPI/crates/Go), sandbox warm-start API. Paid tier ships here — alongside, not instead of, the self-hosted path. *Exit: validation scenarios 9–11 green.*

**Phase 5 — Windows GA + team tier.**
Dev Drive Tier 2 (ReFS clone), NTFS Tier 3, staged fidelity (ADS, sparse); then shared forks, merge queues, fleet timelines — the original 0.1 multi-agent world, built underneath an adopted product.

---

## 17. Business Model

- **Open source, permissive, forever-free:** engine, rewind, fork, merge, MCP, **and multi-machine self-hosted sync**. Local-first means the free tier is genuinely whole; OSS multi-machine is the proof of posture, and every "furrow saved my workspace" story is distribution.
- **Subscription (indie, ~$10–15/mo):** zero-config relay between machines, registry pool CDN hydration, managed encrypted continuous backup, cloud-sandbox warm start. The line: *your state on your machines and your own remotes is free; our infrastructure moving and keeping it is the product.*
- **Team tier (later):** shared forks, merge queues, fleet timelines, org key management.
- **Distribution:** MCP + agent hooks make the fastest-growing dev tools the install channel; "works on any repo, any language, one command" is the README claim — backed by the fidelity matrix, not vibes.

---

## 18. What Changed from Draft 0.2

| 0.2 claim / design | 0.3 disposition |
|---|---|
| CAS under in-repo `.furrow/` | **Fixed:** store in per-user app-data; `.furrow/` is config-only; watcher hard-excludes store paths (Invariant 7, §5.1) |
| "Import usable immediately" with background byte ingest | **Fixed:** complete snapshots only (Invariant 2); prioritized ingest; protection begins at first complete seal with ETA (§9.1) |
| Pool eligibility by `dependency` classification | **Fixed:** provenance gate — lockfile integrity → registry extraction manifests → per-file hash membership, fail-closed; classification is local policy only (§8.4) |
| "Snapshot cost O(changed bytes)" implying O(delta) reads | **Fixed:** honest cost model — O(changed files' bytes) read, O(delta) storage/trees; anchors optimize storage, not reads (§8.1) |
| Database group capture "valid by definition" | **Fixed:** tiered labeled contract — L0 best-effort crash-consistent, L1 SQLite backup API, L2 fs-snapshot point-in-time (§9.4) |
| "Fork < 1 s" via per-file CoW at any size | **Fixed:** O(1) claims restricted to fs-snapshot/overlay tiers; per-file clone is O(entries) with disclosed projections; macOS large-tree reality stated (§8.2) |
| "Entire working state" undefined at the edges | **Fixed:** per-OS fidelity matrix — hard links, xattrs/resource forks, ACLs, sparse, ADS, FIFOs/sockets, submodules, alternates (§6, Invariant 6) |
| Rewind safety via warnings | **Fixed:** mutation lease, agent pause hooks, monitored restore, abort-and-rollback default, explicit `--force` (§9.3) |
| Raw plaintext BLAKE3 as remote chunk identity | **Fixed:** opaque per-account `remote_id` + ciphertext checksums for all non-pool chunks, hosted and self-hosted (§5.2) |
| 150 MiB daemon "across all repos" | **Fixed:** reference configuration (10 repos / 2M entries), disk-backed indexes, +~40 MiB per extra 1M entries, idle-repo hibernation with rescan-on-wake (§10.3) |
| Multi-machine use unaddressed in OSS | **Added:** IP-5 — pairing, SSH and dumb-remote transports, follow mode, leases, sibling convergence via the merge engine; hosted tier repositioned as convenience-only (§8.5, §17) |

---

## 19. Open Questions

1. Pool ecosystem sequencing beyond npm/PyPI/crates/Go; community-seeded pool content is rejected initially on integrity grounds — revisit with signed reproducible extraction.
2. Auto-fork default policy for agent hooks: fork-per-agent, fork-per-task, or opt-in only.
3. Minimum viable check-command UX for the merge gate in repos with no tests (R12's residual).
4. `furrow timeline` Phase-1 surface: TUI, local web UI, or both.
5. Recovery-phrase UX vs. optional social/org escrow for remote-backup keys.
6. Single-writer lease default once Phase 4 lifts the follow-only restriction: keep it mandatory-by-default (Phase-2 behavior, safest "latest" semantics) or relax to opt-in (fewer takeover prompts, more merges)?
7. Windows GA criteria: acceptable NTFS Tier-3 fork latency and minimum fidelity coverage (ADS? sparse?) before launch.
8. Whether Tier-1 overlayfs forks should be promoted to durable directories on `merge` or remain mount-lifetime-bound.
