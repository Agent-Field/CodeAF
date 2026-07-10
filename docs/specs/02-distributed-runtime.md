# furrow — Document 2: Distributed Runtime, Developer Experience, and Commercial Model

**Status:** Draft 0.1 — companion to [Document 1](01-working-state.md), draft 0.3
**Audience:** Product, architecture, infrastructure, and business reviewers
**Scope boundary:** Document 1 owns the local engine and its semantics — snapshots, rewind, fork, merge, sync, retention, fidelity, security of the store. This document owns everything *around* it: ephemeral runners, network architecture, inter-agent coordination primitives, cold-start engineering, the open-source/commercial split, unit economics, and DX/UX contracts. Where the two documents touch (classes, snapshot IDs, transport encryption, phases), Document 1 is authoritative and this document cites it.
**Last updated:** 2026-07-09

---

## 1. Governing Principles

1. **Vendor neutrality is absolute.** Nothing specific to Claude Code, Codex, Cursor, or any agent product enters furrow code. Integration surfaces are generic: filesystem semantics, CLI, hooks (any command can call `furrow snap`), and MCP (an open protocol). Agent-specific glue lives in user config or contrib examples, never in core.
2. **Open code is never artificially slow.** Every algorithm ships in source: hydration planner, prefetch profiles, dedup, memoization, coordination primitives. The commercial tier is faster only through *physics* (compute colocated with state), *shared infrastructure* (registry pool, prewarmed sandboxes), and *absence of setup* — never through withheld or degraded code paths. The open client is complete (§5.1); what it cannot do without us is exactly what requires someone else's presence, not someone else's code.
3. **All connections are outbound.** No participant — laptop, desktop, runner — ever requires an inbound port, NAT traversal, VPN, or Tailscale. Optional direct paths (LAN, tailnet) are auto-detected accelerations; correctness never depends on connectivity luck.
4. **Capability is free; adjacency and absence-of-setup are paid.** The paywall sits exactly on "I could do this myself in ~30 minutes of setup and upkeep, or pay a small amount and not."
5. **The state model does the coordinating.** Multi-agent behavior emerges from versioned filesystem primitives (forks, claims, a coordination class), not from an orchestrator or message bus.
6. **Easy local, easy hosted, serverless self-host.** The client binary is effortless (it is the viral surface). The hosted service is effortless (it is the business). Self-hosting requires **no server of ours at all**: the client federates over the user's own infrastructure — SSH/tailnet for live device↔device sync, any S3-compatible bucket for async — with the protocol and on-disk/bucket formats publicly documented. There is no public server artifact; we operate the only coordination service, and anyone who wants otherwise has commodity tools that already work.

---

## 2. IP-6: Ephemeral Runners (`furrow run`)

### 2.1 What it does

```text
furrow run --cloud 3 "claude -p 'fix issue #42'"      # hosted (paid)
furrow run --host ssh://my-hetzner-box "cargo test"    # BYO compute (OSS)
furrow run --driver docker "npx codemod ..."           # local container (OSS)
```

Lifecycle, identical across drivers:

1. **Seal + push delta.** The current workspace seals (forced boundary, Doc 1 §8.1); only private chunks the rendezvous lacks are uploaded — typically KBs–MBs, since dependency bytes resolve via pool or regeneration and history is deduped.
2. **Provision N sandboxes.** Hosted: prewarmed micro-VMs colocated with the managed store. BYO: any Linux host reachable outbound (SSH, Docker socket, or a cloud driver with the user's API key).
3. **Lazy attach.** Each sandbox mounts a fork of the snapshot via FUSE (or NFS-loopback) — *server-side Linux only*, where lazy mounts are cheap and controlled. An agent that reads 40 files and edits 6 transfers kilobytes. Dev machines never require FUSE (Doc 1's materialization rules stand).
4. **Run + seal continuously.** The command executes; the runner's engine seals to its own fork timeline; logs and progress stream back live.
5. **Runners die; snapshots survive.** Results appear locally as sibling forks: `furrow forks` → diff → `furrow try`/merge one, discard the rest. Sandboxes are destroyed; nothing durable lives on a runner.

### 2.2 Driver interface (OSS)

A small trait: `provision(n, image) / attach(snapshot, fork) / exec(cmd, streams) / destroy()`. Shipped drivers: `ssh`, `docker`, `local`. Cloud drivers (Fly, Hetzner, EC2) are pluggable and community-extensible. The hosted service implements the same interface — the paid product is a driver plus infrastructure, not a fork of the product.

### 2.3 Secrets and keys on runners

- A runner executing code necessarily sees plaintext of what it mounts. Therefore: **session-scoped keys**, granted per run, living only inside the sandbox, dying with it.
- **`config-secret` class is never shipped to runners by default** (Doc 1 §5.9). Specific secrets are allowlisted per-path in `.furrowpolicy` (e.g., a test API key). The default answer to "you run my code with my `.env` on your machines?" is "no — unless you name the exact file."
- Runner-sealed snapshots are signed by the session key; local `furrow forks` shows which universe came from which runner.

### 2.4 Third-party sandboxes as machines (Claude Code web, Codex cloud, any Linux sandbox)

Cloud agent sandboxes normally clone from a git host — they see the last *push*, never the working state. Because they are Linux boxes with outbound internet that permit setup commands, they can become furrow machines with two lines and **no vendor partnership**:

```text
curl -fsSL https://furrow.sh | sh
furrow clone you/myproject --token $FURROW_TOKEN
```

- **Scoped tokens:** per-sandbox, short-lived, fork-scoped — the sandbox writes only to its own fork namespace, can never fast-forward the user's main, and is revocable at any time.
- **State source:** live from the laptop when it's online (P2P/relay via §3.1); otherwise the last seal held in the stored quota — which is exactly what the quota is for.
- **Secrets:** `config-secret` never ships to a third-party sandbox unless explicitly path-allowlisted (§2.3 default applies with extra force here).
- Results seal back and appear locally as sibling forks. Positioning line: *"Your cloud agent works on what's actually on your laptop — not your last push."*

This is Document 3's Capability D arriving early through the front door: it needs none of D's hard parts (no lazy mounts, no seal streaming) — only Phase-4 transport plus token scoping. It is a named Phase-4 deliverable (§8).

### 2.5 Cold start, decomposed

| Layer | Mechanism | OSS | Hosted |
|---|---|---|---|
| Source + config bytes | delta from rendezvous, priority hydration | seconds | seconds |
| Dependencies | planner picks: registry pool / regenerate (`npm ci`, `cargo fetch` — the registries' own CDNs) / fetch from bucket | regeneration-dominated, ~1–3 min | pool bytes incl. postinstall output + build caches, seconds |
| Sandbox boot | user's machine or container | already running / container start | prewarmed micro-VM, ~hundreds of ms |
| Transfer path | user's bucket region ↔ runner | internet RTTs | intra-region, GB/s |
| Prefetch | learned access profiles shipped with the snapshot (OSS algorithm) | ✔ | ✔ + warm CDN cache |

**Targets:** OSS BYO cold start ≤ 3 min (dominated by dependency regeneration — honest, not crippled). Hosted: **command-to-agent-working < 10 s.** The gap is physics and shared infrastructure; both paths run identical open code.

---

## 3. Network Architecture

### 3.1 Rendezvous model

```text
 laptop ──outbound HTTPS──►   rendezvous   ◄──outbound HTTPS── runner / desktop
                        data plane: bucket (S3/R2/B2 API) or managed store
                        control plane: relay (outbound WebSocket both sides —
                        logs, progress, cancellation, lease grants)
```

- **Data plane:** chunks move through the rendezvous — the user's own bucket (OSS) or the managed store (hosted). Plain object-storage HTTPS: works from hotel Wi-Fi, corporate NAT, CI.
- **Control plane:** both endpoints hold outbound WebSockets to a relay that pairs them; that is how a terminal receives live logs from a sandbox neither side can dial. (Same pattern as CI runners and tunnel services — boring, proven.)
- **Self-hosted control:** for pure-OSS `--host ssh://...` runs, the SSH channel *is* the control plane; no relay involved. The relay is only needed when neither side can reach the other — which is exactly the hosted case.
- **Device-to-device data plane (free tier):** when a user's own paired devices are online simultaneously, the signaling service brokers a **hole-punched direct connection** (ICE/QUIC-style; succeeds for the large majority of NAT pairs). Ciphertext then flows laptop↔laptop without touching our infrastructure — we carry kilobytes of signaling and step out. Fallback for hostile NATs: relay-carried ciphertext, soft-capped (§5.4). Machines *not* simultaneously online use store-and-forward against the stored quota or a self-hosted remote. This is a deliberate, narrow reintroduction of P2P — own paired devices only, both online, encrypted end-to-end, never load-bearing for any durability claim.
- **Auto-detected accelerations:** mDNS on shared LANs and existing tailnets are used for direct device↔device transfer when present (Doc 1 §8.5 device-CDN); the rendezvous path always remains as fallback. Tailscale is a silent speed boost, never a prerequisite.

### 3.2 DX tiers (simplest first)

| Tier | Setup | Cost | What works |
|---|---|---|---|
| Hosted | `furrow login` — once | free tier (§5.4), subscription + per-minute runners | everything, zero config: live P2P sync, relay, stored quota, pool, runners |
| BYO bucket | `furrow remote add r2://…` — paste one URL + key | free; cents of storage | **async** sync + backup (a dumb bucket has no live coordination — by physics), BYO runners against your bucket |
| BYO compute | `furrow run --host ssh://box` or a cloud API key | free; your hardware | runners without any hosted component; SSH is its own control plane |
| Local only | install binary | free | rewind, fork, exec, try, bisect, shrink, merge, MCP — unlimited repos |

Every tier above "local only" degrades gracefully to the tier below it when offline.

### 3.3 Wire protocol performance contract

Discovered empirically (initial SSH publish: 3+ minutes, near-zero CPU — per-object acknowledgment on a ~100 ms WAN): **round trips, not bandwidth or hashing, dominate content-addressed sync.** The protocol is therefore governed by three laws and a test gate.

**Law 1 — never wait per object.**
- One logical stream, windowed and pipelined: sender streams continuously against a byte-based in-flight window (32–64 MiB); receiver returns *cumulative* acknowledgments (durability frontier: "everything through frame N is fsynced"). Acks are flow control, never confirmation gates. Content addressing makes optimistic sending safe — a duplicated object is idempotent, so resend-on-doubt beats ask-before-send.
- Seal completion binds to the receiver's durable frontier (group fsync per frame, not per object), preserving Doc 1's journal-after-durable ordering with batched cost.

**Law 2 — negotiate in O(1) round trips, not O(objects).**
- Common case (incremental publish): **ancestry delta** — receiver states its frontier ("I have seal S42"); the DAG makes "everything since S42" exactly computable sender-side. Zero per-object negotiation.
- Cold/divergent case: Merkle-subtree pruning (matching tree roots eliminate whole subtrees), and for unknown-overlap sets, **one-round-trip set reconciliation** (IBLT/minisketch-class, as in Bitcoin's Erlay): wire cost proportional to the *difference*, not the set.
- Reconnect: persist a per-remote known-have frontier locally as a hint (**zero-RTT resume**); correctness comes from idempotent puts and receiver acks, never from the hint.

**Law 3 — the fastest byte is the one not sent.**
- Chunks are compressed (zstd, class-aware) *before* encryption at rest — encrypted bytes are incompressible, so compression must live below the crypto boundary. Manifest/index frames use a zstd dictionary.
- Pack frames: small objects (source files are whole-file chunks) coalesce into ~4–8 MiB indexed frames appended directly to receiver pack files — never one write, one ack, or one fsync per tiny object.
- Priority hydration order on the wire: manifests + `source`/`config` first (remote side usable in seconds), bulk classes streamed behind — perceived publish/clone latency detaches from total bytes, and progress states it honestly.

**Transport substrate.** SSH is bootstrap and fallback, one persistent channel, custom framing, channel window raised (default ~2 MiB SSH windows stall high-BDP links). Target substrate is **QUIC** (device-key TLS): no head-of-line blocking, 0-RTT reconnection, connection migration (Wi-Fi→hotspot mid-sync survives), BBR congestion control, stream multiplexing for priority lanes. Never a process or exec per object under any substrate.

**Law 4 — pay connection costs once, then push.** (From WAN measurement: 530 KB taking 4 s while 3.8 MB took 5.3 s — the signature of fixed per-operation costs.)
- One **persistent, bidirectional session per remote**, kept alive, carrying all operations: no per-operation dial, no cold TCP slow-start on every transfer, warm congestion window amortized across the workflow.
- **Push, never poll, on a live session:** publish emits a notify frame to subscribed peers; the reverse pull rides the same hot connection with a single-flight want-list. A materialize that waits on a poll tick is a protocol bug, not a tuning issue.
- Phases overlap: the ancestry frontier travels *with* the first optimistic data frame (idempotent puts make this safe); finalize rides the cumulative ack already in flight.
- **`--timings` is mandatory instrumentation:** every transport operation can emit its phase breakdown (connect / auth / negotiate / stream / fsync-wait / notify), and perf work begins by comparing against a raw `scp`/`iperf` floor on the same path — never by guessing the layer.

**Law 5 — the receiver adopts, it never re-earns.** (From measurement: warm wire phases ≤ 1.4 s but end-to-end usability 5–8 s — the gap was entirely receiver-side apply and re-seal work.)
- An incoming snapshot is already sealed, named, and chunk-verified: apply ends with **fast-forward adoption** of that snapshot ID as the local head. Re-chunking, re-hashing, or re-sealing a received state is a protocol bug, not overhead.
- **Delta materialization only:** tree-diff local head vs. incoming head and touch changed paths, nothing else. The clean-follower case skips the pre-apply divergence walk entirely — the watcher's dirty set answers "did anything change locally" in O(1).
- **Self-write suppression is mandatory:** the applier's writes are invisible to the watcher, and the new baseline (size/mtime/inode recorded per file *during* the write) is installed so the dirty set clears with zero re-reads. An apply that triggers a seal that triggers a publish is an echo loop.
- Apply overlaps transfer — files materialize as their chunks complete, hiding apply latency inside stream time — and fsyncs in batches against the durability frontier, not per file.
- Apply emits the same `--timings` discipline: `diff-compute / divergence-check / write / fsync / baseline-install / watcher-requiesce`.

**Test gate (Validation).** A latency-injected rig (netem: 100 ms RTT, 1% loss, 50 Mbps) is a required CI fixture — the Docker-local test that missed this failure is insufficient by construction. Gates: incremental publish of a 100-changed-file seal ≤ 3 s at 100 ms RTT; cold publish within 1.2× of bandwidth-optimal for bytes actually sent; total protocol round trips for any publish ≤ 5 regardless of object count; **warm-session small delta (≤ 1 MB) ≤ 3 RTTs + transfer time; publish-to-remote-notify ≤ 1 RTT after durability; second operation on a session pays zero connection cost; receiver apply for a small delta ≤ 500 ms beyond stream completion on a clean follower, with zero re-hash of received content and zero watcher-triggered re-seal.**

---

## 4. IP-7: Inter-Agent Coordination Primitives

Agents coordinate **through versioned state, not APIs** — filesystem-level, so anything that can read and write files (or speak MCP) can participate. All primitives are OSS.

- **`furrow claim <glob>`** — advisory path leases recorded in the DAG. Agent A claims `src/auth/**`; agent B's overlapping claim is refused with A's identity and claim time. Work partitioning with zero orchestrator. Claims expire with the fork or by TTL; they are advisory (the engine never blocks writes — it makes contention *visible*, and merge classification uses claim history as a signal).
- **`coord` content class** — a designated directory (default `.furrow/coord/`) replicated *eagerly* between sibling forks — not waiting for quiescence seals. Agents leave task lists, notes, partial results; it is a versioned blackboard with the same history, rewind, and attribution as everything else.
- **`furrow watch-fork <name>`** — subscribe to a sibling fork's seals (CLI stream and MCP notification): "agent B just sealed changes to the file you are editing."
- **MCP surface:** `claim`, `release`, `coord-read/write`, `watch-fork`, `fork-diff` — the complete multi-agent toolkit exposed through one open protocol, with zero knowledge of which agent product is calling.

**Colocation acceleration (hosted, by physics):** sibling forks on the same runner host share one local CAS, so coord-class propagation and watch-fork latency drop from bucket-round-trip to milliseconds. Same primitives, faster adjacency — consistent with Principle 2.

---

## 5. Source Strategy and Commercial Model

The posture is the Sentry/Lago model, adapted: **easy local, easy hosted, possible self-host** (Principle 6). We take the full benefit of open source — trust, distribution, packaging, contributions, embedding, the dead-man's answer — while the operationally natural path to multi-machine and cloud features is `furrow login`.

### 5.1 The two artifacts and their postures

| Artifact | License | Posture |
|---|---|---|
| **Client/engine** (binary, CLI, MCP adapter, Mission Control, SSH/LAN device sync, bucket remotes, BYO runner drivers — plus the documented protocol and on-disk/bucket formats) | Apache-2.0 | **Effortless, polished, forever-free, and genuinely complete.** Everything that can happen on or between the user's own machines lives here — including the entire parallel-agent product and serverless multi-machine sync. This is the viral surface and the embed channel (Plaid play). Defaults wire to the hosted rendezvous; `furrow login` is the golden path printed by the installer. |
| **`furrow-cloud`** (signaling/hole-punch brokering, relay, stored quota, identity/SSO, tenancy, billing, sandbox token issuance, account console, registry pool, prewarmed runner fleet, specialized subharness recipes) | closed | **The only server anywhere in the product — and it's ours.** Partly service code, partly operations-and-data that cannot be downloaded because they are not software (pool content, fleet, global presence). No public server artifact exists: self-hosters need none (Principle 6), so there is nothing to license-lawyer, no crippled reference server to criticize, and no enterprise-auth burden in public. |

The boundary is **natural, never inserted** (Principle 6): the client is complete for everything the user's own infrastructure can reach; our service exists precisely for what it can't — machines that can't dial each other, machines that aren't awake together, and shared assets (pool, fleet). The lock-in answer lives in the client plus documented formats, not in a server: any bucket written by furrow is readable forever with the Apache client alone. If the community builds a third-party coordination server against the documented protocol (the Headscale pattern), that validates the protocol and is welcome — it will still have none of our data assets or fleet.

### 5.2 What each path gets

| | Client alone (free, offline-capable) | + Hosted (`furrow login`) | Self-managed (client + own infra) |
|---|---|---|---|
| Engine: rewind, fork, exec, try, bisect, shrink, merge, timeline, MCP, Mission Control | ✔ unlimited repos | ✔ | ✔ |
| Parallel agents: universes, radar, merge preview, coordination | ✔ fully local | ✔ | ✔ |
| Live device↔device sync | ✔ same LAN | ✔ across any NATs, P2P-brokered (§3.1) | ✔ via own SSH/tailnet reachability |
| Async sync + backup | — | ✔ stored quota → subscription | ✔ own bucket |
| Third-party sandbox access (§2.4) | — | ✔ scoped tokens, laptop may sleep | possible with own bucket/tailnet creds in the sandbox (user manages secrets exposure) |
| Registry pool hydration | regeneration fallback (registries' own CDNs) | ✔ CDN, exact bytes | regeneration fallback |
| Runners | BYO SSH/Docker | ✔ prewarmed, per-minute | BYO |

### 5.3 Free tier — meter cost, never usage

- **Unlimited** repos, folders, devices, and all local features. Local COGS is zero; every watched folder deepens the habit and widens the future attach surface.
- **Live P2P sync between paired devices: free and unmetered** — signaling costs us kilobytes; data flows hole-punched device↔device (§3.1).
- **Relay fallback: soft-capped** (~10 GB/month, throttled after) — real but small cost, minority of connections, cap prevents tunneling abuse.
- **Stored quota: 1 GB free** — the async-handoff and backup taste, and the on-ramp to the storage subscription, because async sync, durable backup, and runner/subharness access to a sleeping laptop's state are all the same capability: *someone holding your encrypted bytes when your machines can't.*
- The free/paid boundary in one sentence: **live mirroring is free because we're not needed; persistence is paid because someone must be there when you're not.**

### 5.4 Friction audit — does the open code still go viral?

The viral surface — `shrink` freeing 50 GB, `try` making any risky command reversible, bisect answering "what broke it", `exec` running parallel agents on one laptop — carries **zero setup and zero account**: one binary, offline. Those spread on their own; every rescue and every disk-space screenshot is distribution. And the *first* multi-machine taste is now also zero-setup (`furrow login`, free live sync), so the demo moment no longer requires a bucket ceremony. Self-hosting frictions gate exactly the population that was never going to pay — while their existence keeps the "you're not locked in" answer true, which is worth more than their revenue.

### 5.5 Unit economics (hosted, indie ~$12/mo + metered runner minutes)

- **Signaling:** kilobytes per session; effectively free at any scale.
- **Relay:** bandwidth on the ~10–15% of hostile-NAT connections, soft-capped on free.
- **Sync/backup storage:** deduped, delta-based ciphertext; a heavy user holds ~10–20 GB → COGS ≈ $0.15–0.30/mo on zero-egress storage. ~97% gross margin on the subscription core.
- **Registry pool:** one shared copy of each public package version serves every customer; CDN cache-hit-dominated. **Cost per user falls as users grow** — inverse of typical infra scaling.
- **Runners:** cost-plus per-minute over commodity micro-VMs; subscription includes N free minutes (habit formation), margin in metered overage. Main idle-cost risk is the prewarm pool → demand-based sizing (§7, R5).
- **Free tier COGS:** cents — signaling plus capped relay plus 1 GB of deduped ciphertext.

### 5.6 Repository and product layout (where SSO, billing, and the SaaS UI live)

Two repos, mirroring the two artifacts of §5.1 — the Tailscale shape (open client, closed control plane), simpler than the Sentry/Lago three-way split because no public server exists:

| Repo | Visibility / license | Contents |
|---|---|---|
| `furrow` | public, Apache-2.0, single license for the whole repo | Client/engine, CLI, MCP adapter, **Mission Control** (the local daemon-served UI, Doc 3 §7 — ships in the binary, open), SSH/LAN device sync, bucket remotes, BYO runner drivers, the *client side* of `furrow login` (OAuth device-code flow, auditable), and `docs/protocol/` — the documented wire protocol and on-disk/bucket formats (the lock-in answer and the dead-man's switch) |
| `furrow-cloud` | private, closed | Everything served: signaling/hole-punch brokering, relay, stored-quota storage, identity (OAuth/SSO sign-in, device linking), multi-tenancy, orgs/teams, billing and quotas, sandbox token issuance (§2.4), the **hosted account console** (devices, usage, tokens, billing), pool tooling, fleet orchestration, admin. Speaks the public protocol; the client repo's protocol tests are the compatibility contract |

Notes:
- **Two UIs, deliberately distinct — and only one of them exists as a public artifact:** Mission Control (open, local, per-machine, renders CLI JSON — Doc 3 §7) is about the *workspace*; the account console (closed, hosted) is about the *account* (devices, tokens, quotas, billing). There is no third "server admin UI" because there is no public server. Neither UI ever grows the other's job.
- **`furrow login` is open code talking to a closed service.** Self-managed users simply never call it — their client federates over SSH/tailnet/buckets with no account at all.
- **SSO/SAML lives only in `furrow-cloud`** as a team-tier feature — no pressure to ever maintain enterprise auth in public.
- Single license per repo — no per-directory licensing, no GitLab-EE-style enterprise folders, nothing to lawyer. The open repo is honestly and entirely Apache.

---

## 6. DX / UX Contracts

1. **One-command onboarding per tier:** `furrow login` (hosted) or `furrow remote add <url>` (BYO) unlock their whole tier; no further configuration is required for defaults to be safe and useful.
2. **Cost is disclosed before it is incurred:** `furrow run` prints sandbox count, estimated minutes (from prior runs when available), and hydration plan (pool/regenerate/fetch split) before provisioning; `furrow teleport` prints estimated upload bytes. No surprise bills, no surprise waits (extends Doc 1 Invariant 11 to money and minutes).
3. **Live, attributable output:** every runner streams logs prefixed by fork name; `furrow forks` is the single pane for what every universe is doing and what it costs.
4. **Failure is legible:** a dead runner leaves its last sealed snapshot and a terminal status (`completed | failed(exit) | lost(timeout)`); partial work is never silently discarded — it is a sibling fork like any other.
5. **Offline never breaks local:** losing connectivity mid-run means results arrive when connectivity returns; local features are untouched (Doc 1 Invariant 13).
6. **Agent-facing surfaces are explicit:** runners and agents address snapshots by ID, destructive operations require explicit flags (Doc 1 §9.3), and MCP tools mirror CLI semantics one-to-one — no agent-only behavior.

---

## 7. Risks Specific to This Document

| # | Risk | Resolution | Residual |
|---|---|---|---|
| R1 | Runner sees plaintext (inherent to executing code) | Session-scoped keys dying with the sandbox; `config-secret` never shipped unless path-allowlisted; signed runner seals (§2.3) | Users must understand allowlisting = disclosure to the sandbox; stated at the prompt |
| R2 | Free/cheap runner minutes attract abuse (mining, spam) | Runner egress restricted to registries + rendezvous by default; minute caps on new accounts; workload heuristics | Arms race; standard for compute products |
| R3 | Relay becomes an availability choke point | Relay is control-plane only (data goes bucket-direct); SSH tier bypasses it entirely; stateless relay scales horizontally | Hosted live-logs degrade during relay outage; runs complete regardless |
| R4 | Pool coverage gaps (ecosystems, private mirrors) | Planner's regeneration fallback is a first-class path, not an error; coverage sequencing npm → PyPI → crates → Go (Doc 1 §19.1) | Uncovered ecosystems get OSS-grade cold start on hosted too — disclosed in the plan output |
| R5 | Prewarm pool idle cost inverts runner margins | Demand-based pool sizing; micro-VM boot is fast enough (~sub-second) that the pool can run shallow; include-minutes tuned to observed utilization | Cold-start target degrades to ~30 s under pool misses at demand spikes |
| R6 | BYO cloud drivers rot (API churn) | Drivers are a thin OSS trait with community ownership; `ssh` and `docker` (stable interfaces) are the maintained reference paths | Exotic drivers may lag; core never depends on them |
| R7 | Coordination primitives race (claim conflicts across partitions) | Claims are advisory + DAG-recorded: conflicts are *detected and visible*, never load-bearing for correctness; merge uses claim history as a signal (§4) | Two offline agents can still both claim a path; surfaced at sync, resolved at merge — consistent with Doc 1 sibling semantics |
| R8 | "Open source as bait" criticism | Strongest possible answer by construction: the Apache client is *complete* — the entire parallel-agent product, serverless multi-machine sync, and bucket backup all work with no account; there is no crippled public server to point at because no public server exists; protocol and formats are documented so data is readable forever with the client alone; Principle 2 enforced in CI (no tier-branched performance) | A community coordination server may appear (Headscale pattern) — welcomed, validates the protocol, carries none of our data assets or fleet |

---

## 8. Delivery Mapping (against Document 1 §16)

| Doc 1 phase | This document adds |
|---|---|
| Phase 0–1 (POC, engine) | Nothing — this document intentionally has no Phase-0/1 footprint; the Codex worker's current scope is unaffected |
| Phase 2 (fork, MCP, follow-only sync) | Coordination primitives v0 (`claim`, `coord` class, `watch-fork`) — they are local/DAG features and strengthen the parallel-agent demo |
| Phase 3 (merge) | Claim-history as a merge classification signal |
| Phase 4 (full sync, teleport, hosted plane) | Runner driver interface + `ssh`/`docker` drivers (open); signaling + hole-punched device sync + capped relay (free tier, §3.1/§5.3); **third-party sandbox tokens** (§2.4 — likely the flagship demo); hosted runners + prewarm pool + session keys; pool CDN; pricing launch |
| Phase 5 (Windows, team tier) | Colocated multi-runner CAS sharing; team runner quotas |

---

## 9. Open Questions

1. Default runner image contents (language runtimes preinstalled vs. hydrated per-project via the planner) — image weight vs. cold-start variance.
2. Whether `furrow run` without `--cloud`/`--host` should default to the `docker` driver locally (safest demo) or refuse (least surprise).
3. Included-minutes quantity in the base subscription vs. pure metering — habit formation vs. margin predictability.
4. Claim TTL defaults and whether claims should optionally *block* MCP-mediated writes (strict mode) despite the advisory-only engine stance.
5. Relay protocol: reuse an existing open tunnel protocol vs. minimal bespoke WebSocket framing (leaning bespoke-minimal; scope discipline).
6. Whether prefetch access-profiles are shared to the pool anonymously (better cold starts for everyone) or remain strictly per-account (simpler privacy story) — default: per-account.
