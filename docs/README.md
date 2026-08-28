# aforge — the documentation

One page that says where everything is. Start here.

## If you want to…

| …then read | |
| --- | --- |
| **Drive it from a script, CI, or a benchmark** | [HEADLESS.md](HEADLESS.md) — `aforge do` and `aforge exec`, exit codes, the `--json` objects, environment, and the rules for measuring it |
| **Know what it can do, feature by feature** | [FEATURES.md](FEATURES.md) — the master catalog, one entry per capability |
| **Understand how it is built** | [ARCHITECTURE.md](ARCHITECTURE.md) — the store, the event log, the permanent spine, the resident role |
| **Know what it promises a person** | [JOURNEY.md](JOURNEY.md) — the product law: one mouth, and the journeys we hold ourselves to |
| **Measure it** | [../BENCHMARKS.md](../BENCHMARKS.md) — the record of what came back, with its caveats |
| **Keep it fast** | [../PERF.md](../PERF.md) — the size ratchet, the allocation laws, the launch-path pins, and why none of them is a stopwatch |

## The subsystems

| Doc | What it covers |
| --- | --- |
| [STANDING.md](STANDING.md) | Standing goals — recognition not declaration, ratification, the ambient rail, the watch |
| [LEARNING.md](LEARNING.md) | Learning that compounds — the notebook, the skill forge, experiments, playbooks, the router |
| [META.md](META.md) | Meta-learning — the journal as training data about the learning itself |
| [JOBS.md](JOBS.md) | Background jobs — two primitives, turn-boundary reporting, nothing outlives its leaf |
| [SERVICES.md](SERVICES.md) | Services — long-running processes the user meant to keep; promotion is consent |
| [MULTIMODAL.md](MULTIMODAL.md) | Voice, images, documents, media generation — capability for the graph, presence for the chat |
| [THREAD-UX.md](THREAD-UX.md) | The thread and the cards — one conversation, many living jobs |
| [SUBHARNESS.md](SUBHARNESS.md) | Node harnesses — one task graph, many workers: ours, claude, codex |

## Two rules about these files

**The runtime source of truth is `internal/manual`, not `docs/`.** These pages
are design documents: written to persuade, on disk, stale the moment a build
lands. What aforge *says about itself* comes from the user-voice pages embedded
in the binary and searched by the `manual` tool. Where they disagree, the
manual wins. Add a feature, add its manual paragraph; update these afterwards
if you like, but never instead.

**Where a doc and the binary disagree about behaviour, the binary wins and the
doc is a bug.** [HEADLESS.md](HEADLESS.md) in particular is a contract other
programs are written against — its exit codes and JSON field meanings are
promises, and changing them is a breaking change to every harness.
