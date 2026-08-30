# The Four Pillars — Detailed Feature Design

*Date: 2026-08-30. Status: ideation. Companion to `deep-terminal-capabilities.md`.*

The four capabilities picked out for deepest exploration, translated into
what the user actually experiences.

## 1. Network namespace isolation → "the agent can never leak, and runs are reproducible"

What the user experiences: every agent task gets a declared network profile
(`--net=deps-only`, `--net=none`, `--net=full`). A task running tests
physically cannot phone home, cannot exfiltrate your `.env`, cannot hit a
surprise endpoint that makes a run non-deterministic.

- Credentials live in the proxy, not the process — the agent never holds
  your real GitHub token; the proxy injects it only for `api.github.com`.
- All HTTP goes through the recording proxy, so a run that hit the network
  yesterday can be replayed offline today, byte-identical.
- For an indie dev: run agents on client codebases without a legal headache,
  and flaky-network test suites become deterministic.

Mechanisms: Linux network namespaces + veth pairs + a localhost MITM proxy;
on macOS, sandbox-exec profiles + pf rules (weaker but workable).

## 2. io_uring batch engine → "repo-scale operations are transactions, and they're instant"

What the user experiences: a 4,000-file rename lands in under a second, and
either *all* of it lands or none — no more "agent crashed halfway through
the refactor, repo is in a broken state."

- Combined with snapshots, every mutating operation becomes a database
  transaction over the filesystem.
- The user-facing feature is **fearless parallelism**: run 5 agents on 5
  approaches simultaneously because each is cheap, isolated, and rollback
  is free.
- The engineering: registered buffers, linked SQEs, and a journal for crash
  recovery. On macOS, kqueue + clonefile stand in for io_uring.

## 3. Shared-memory blackboard → "agents that actually coordinate, at memory speed"

What the user experiences: when you run parallel tasks, they don't stomp
each other. Task A claims `src/auth/` on the blackboard; task B sees the
claim in microseconds and works elsewhere. A flaky-test discovery by one
task is instantly known to all.

- No file polling, no lock files, no merge collisions at the end.
- The difference between "5 agents in 5 folders" (what everyone does) and
  "5 agents in one shared mind."
- The kernel-knowledge part: an mmap'd region with a lock-free ring buffer
  (LMAX Disruptor style), sequence numbers + CAS, wait-free readers,
  futexes for blocking consumers, and crash-safety — a dead agent's claims
  must expire (pidfd/kevent EVFILT_PROC watcher to auto-revoke).

## 4. Kernel-scheduler awareness → "your machine stays fast while agents work"

What the user experiences: on an M-series Mac, agent compile jobs get
steered to efficiency cores so the editor and browser stay on performance
cores — the laptop doesn't turn into a space heater.

- The harness notices thrashing (page-fault rates, memory pressure) and
  queues the 6th task instead of starting it.
- Uses `sched_getaffinity`, cgroup CPU shares, `perf_event_open` counters,
  QoS classes on macOS.
- For an indie dev on one laptop: the difference between "agents run in the
  background while I work" and "agents take my machine hostage."

## Dependency order

Snapshots + io_uring transactions → network proxy + netns → blackboard +
scheduler. Each layer enables the next; parallelism is the payoff that the
last two depend on.
