# agit Performance Baselines

This document records reproducible measurements, not extrapolated product claims. The harness is `benches/engine.rs`; every sample runs in a fresh subprocess so CPU and peak RSS are isolated from other scenarios.

## Run It

```bash
cargo bench --bench engine
AGIT_BENCH_ENFORCE=1 cargo bench --bench engine
AGIT_BENCH_PROFILE=reference cargo bench --bench engine
```

The default developer profile uses three samples, 5,000 files, 100 changed files, a 128 MiB generated stream, a 32 MiB warm dependency, 721 snapshots spread over six months, and 1,000 tree/ref lookups. CI uses one smaller enforced smoke sample on macOS and Linux. The `reference` profile uses five samples, 1,000,000 files, a 1 GiB stream, and 17,281 historical snapshots.

Peak RSS comes from `getrusage(RUSAGE_SELF)`. CPU is user plus system time consumed inside the measured operation. Fixture construction, Git initialization, and baseline capture are outside wall/CPU timing but remain included in the subprocess peak-RSS measurement.

## Baseline: 2026-07-09

Hardware: Apple arm64 `Mac15,10`, APFS, macOS 26.5.1. Rust release profile uses thin LTO, one codegen unit, and `opt-level=3`.

| Scenario | Dataset | Median | p95 | CPU p95 | Peak RSS | Minimum rate |
|---|---:|---:|---:|---:|---:|---:|
| Chunk + BLAKE3 | 128 MiB | 240.9 ms | 244.8 ms | 239.3 ms | 7.8 MiB | 522.9 MiB/s |
| Paged tree diff | 5k entries, one change, 1k runs | 1.371 s | 1.378 s | 1.347 s | 9.7 MiB | 1.38 ms/run |
| Reverse ref lookup | 721 refs, 20 returned, 1k runs | 176.8 ms | 188.8 ms | 181.7 ms | 6.4 MiB | 0.189 ms/read |
| Cold seal | 5k files | 967.1 ms | 1.065 s | 778.1 ms | 12.8 MiB | 4,693 files/s |
| Delta seal | 100 of 5k files | 15.3 ms | 16.1 ms | 2.1 ms | 13.1 MiB | 6,202 files/s |
| Full-state fork | 5k files + 32 MiB warm cache | 727.7 ms | 751.4 ms | 621.7 ms | 18.3 MiB | 6,693 entries/s |
| Five warm universes | 5 × (5k files + shared 32 MiB cache) | 1.242 s | 1.270 s | 837.9 ms | 18.6 MiB | 3.94 universes/s |
| Conflict radar | 5 forks × 100 overlapping paths | 543.6 ms | 548.1 ms | 524.1 ms | 18.0 MiB | 912 dirty memberships/s |
| Retention + exact GC | 721 snapshots / six months | 78.0 ms | 87.8 ms | 60.2 ms | 9.1 MiB | 8,216 snapshots/s |

The universe row covers five verified CoW materializations plus five concurrent process launches through the macOS sibling-directory driver. It measures startup cost, not page-cache sharing.

The radar row is a cold family-index reconciliation from five independently sealed heads. Its p95 no-change refresh was 52.5 ms. Dirty scopes and conflict membership stay in SQLite; the process never constructs a repository-sized cross-fork map.

The fork's p95 inner atomic hierarchy clone was 133 ms. The remaining time is bounded metadata/cache proof, path-index backup, and durable publication.

## Optimization Evidence

The same 20,000-file + 32 MiB fork scenario was measured while optimizing the macOS path:

| Implementation | Wall | Inner clone | Peak RSS |
|---|---:|---:|---:|
| Per-file clone + full destination recapture | 8.822 s | 3.572 s | 33.8 MiB |
| Atomic recursive clone + serial proof | 2.323 s | 425 ms | 18.5 MiB |

This is a 3.8x wall-time improvement and a 45% peak-RSS reduction. The fast path uses one atomic recursive `clonefile`, repairs directory metadata, rejects hard-link/special-file cases to the fidelity-preserving fallback, proves every indexed path against capture metadata and cached blob identity, rejects extra/missing paths, and then reuses the already-durable base DAG. Policy-excluded trees use the conservative verifier.

## Gates And Gaps

`AGIT_BENCH_ENFORCE=1` currently requires at least 50 MiB/s chunk throughput, a 2 s delta seal ceiling, at most 100 ms per paged-tree diff, at most 5 ms per indexed ref read, tier-aware fork and multi-universe ceilings, a 10 s conflict-radar ceiling, a 10 s GC ceiling, and at most 512 MiB peak RSS per isolated scenario.

Still unproven and therefore not claimed as achieved:

- The full 1M-file reference profile on both declared reference machines.
- Idle watcher `<1% CPU / <=150 MiB RSS` across 10 repos and 2M entries.
- The complete Btrfs/ZFS/overlayfs/APFS/XFS platform matrix.
- Ten-universe physical-memory/page-cache sharing. The harness measures startup wall time, CPU, and process RSS, but the `~1.0x` shared-page-cache goal remains unclaimed until resident pages are measured on Linux CoW filesystems.
- Generic flat-directory Merkle diff is proportional to root page count; agent delta sealing avoids that path through the disk-backed changed-path index.
- Fork verification remains O(entries) without a true immutable filesystem snapshot, even when hierarchy creation is atomic.

## Warm SSH Session Smoke Baseline: 2026-07-10

The CLI integration fixture runs two independent repositories and object stores
through the real framed SSH-helper protocol, using a local wrapper in place of
network transport. A peer publishes a small changed snapshot while the other
side is blocked on the persistent HEAD subscription. The measured publish start
through notification, pull, and workspace materialization was **255 ms** on the
development Mac. The same test asserts one SSH helper process across multiple
reconciliation cycles, a measured notification phase, and
`reused_connection=true` on the second operation.

This is a local protocol regression baseline, not a WAN result. Run the same
test under latency/loss shaping before claiming the 100 ms RTT contract:

```bash
cargo test --test cli persistent_ssh_helper_syncs_independent_stores_over_framed_stdio -- --nocapture
```
