---
kind: added
title: the conversation battery can run two builds of this binary against each other
pr: 995
surface: [build]
invalidates:
  - "An arm in `bench/conversation` named a harness, so comparing two builds of
    aforge meant two adapters or two checkouts. An arm may now name a BUILD:
    `aforge@dev` and `aforge@simplify` go through the one aforge module and are
    bound to their binaries with `run.sh --bin <arm>=<path>`."
  - "`campaign.py` accepted only `aforge,pi,omp` and told each arm its binary
    through `<ARM>_BIN`. It takes labelled arms, binds them with `--bin`, takes
    `--model`, `--effort` and `--max-cost`, and REFUSES a plan whose two arms
    are the same file — two arms that are one binary report a dead heat."
  - "`summary.sh` compared arms on mean cost and mean wall and printed only
    aggregates. The front is drawn on MEDIANS with a stated rule (no worse on
    all three, better on one), a tie is said out loud, every attempt is printed
    as its own row, and when the mean reverses the median's verdict the report
    says so on the line where the claim is made."
  - "A row carried wall-to-done and nothing about the shape of the work. Rows
    now carry appended columns: seconds to the first word, tool calls, rounds,
    whether a task was spawned, files changed, and the workspace's own build,
    vet and focused-test exits."
  - "A turn plan could only be timed against a marker (`ready`, `midwork`,
    `busy`, `idle`). `after:N` sends a message N seconds after the previous one,
    whatever the screen is doing, which is how a person actually steers."
---

The wave this serves exists because of one turn: on 2026-09-11 a paragraph of UI
work cost seventeen minutes, sixty-two tool calls and forty rounds on `dev`, and
then spilled into a worker task. Five traces found mechanisms rather than a
model. Whether the fixes worked is not a thing a diff can answer, so the battery
learned to ask it directly — the same request, the same model, the same effort,
the same configuration, two binaries.

`bench/conversation/ab.sh` is the whole grid in one command on the Spark:
two detached worktrees, `make build` in each, the binaries installed by absolute
path, a refusal if they come out byte-identical, the Go build cache warmed over
the pinned tree so neither arm pays for it, `campaign.py`'s randomized complete
blocks, and the evidence rsynced back. `--dry-run` prints every command and
spends nothing.

Four scenarios carry the ask. The workspace is a fresh one-commit clone of this
repository at a pinned sha, the harness is launched from a directory that is not
the repository, and the first message names the project by nickname — which is
how the turn under study began. The judge reads the clone afterwards and never
the reply.
