# World-grounded planning — design

The planner is blind by design: every pass in `internal/plan` is one text-in /
JSON-out completion, and sight lives only in the workers (`internal/exec`) and
the head. That buys the four-round build and the byte-stable prefix cache. The
cost is that grounding settles scope by fiat where a fact was one read away,
and the revise sentinel judges contradictions against artifacts it is told
exist but cannot open.

This design closes the gap without adding a serial round, a recon phase, or an
agent loop inside planning. Three channels, one loop.

## Not a coding feature

aforge is a general-purpose harness. Nothing here may assume the workspace is
a repository or the material is source code. "Terrain" is whatever material
the run stands on: a corpus of PDFs, a data directory, fetched pages, a
half-written report, or nothing at all. Git information is one optional line
that appears only when git is present. Prompts say "material" and
"workspace", never "code" or "repo". An empty workspace renders zero bytes and
every prompt stays byte-identical to today — the standing compat pattern.

## Wave 1 — Terrain (pure code, zero model calls)

`plan.RenderTerrain(dir, goal) string`: a deterministic snapshot rendered once
at build start and frozen.

- Depth-1 listing with per-directory rollups: `data/  41 files (.csv, .json)`.
- Cue-directed depth-2: only directories whose names match goal terms expand
  one level further. Deterministic string matching, no model.
- First lines of any README-like file; one git head+status line iff git repo.
- Hard cap ~2KB, rune-boundary clipping, stable ordering (sorted, never map
  iteration).

Carried as `Options.Terrain` → `Graph.Terrain`, rendered into
`graph.context()` between Goal and Settled — one insertion point, inherited by
every pass through the shared prefix, paid warm after the first call.

Invariants: snapshotted once per build, never re-rendered mid-pass (prefix
cache); empty terrain → byte-identical prompts; callers without a workspace
pass nothing.

## Wave 3 — Sighted sentinel (pure code, no tool loop)

`RevisionEvent` and `OverrunGoal` (internal/resident) stop naming artifacts
they cannot show. Code renders, per named artifact: head ~600B at a rune
boundary; beyond a ~2KB total cap, paths+sizes only. Plus a terrain delta:
files added/changed in the workspace since plan time (needs the plan-time
snapshot list, carried on the job). Revise stays one call.

## Wave 4 — Provisional settlements → JIT check

Grounding marks each settled point `by: fact | fiat`. Fiat settlements are
provisional. The stage-1 orientation node's brief gains one line: record
anything that contradicts the settled list. When it lands, the existing revise
trigger fires with Wave-3 digests and can actually verify. When nothing is
provisional and the result names no contradiction, skip the revise call
entirely — an adaptation loop that also removes calls.

## Wave 2 — Sighted ground (last; the only tool-touching wave)

`GroundWith` gains read-only tools (read/list/grep subset + web), hard cap 4
turns, armed only when a code heuristic says the world is relevant (non-empty
workspace, or the goal names URLs/paths). Prompt delta: "where the workspace
or a named source answers a scoping question, read the answer instead of
deciding it." Unarmed runs are byte-identical. Ground already runs concurrent
with spine and already gates fan-out, so sight here costs no rounds.

## Why not a pre-plan recon harness

A "quick planning session that gathers things first" is a serial recon phase:
wall clock before any leaf launches, duplicating the stage-1 orientation node
`fanout.go` already plans for. Doctrine: plan the recon, don't perform it.
Wave 2 is the minimal legitimate form of the instinct.

## Build order and co-working

Order: 1 → 3 → 4 → 2. Waves 1 and 3 are disjoint packages (plan / resident)
and build in parallel worktrees off the current head.

A separate session is rebuilding chat in this same checkout (chat-v2,
uncommitted work in cmd/aforge/chat.go, internal/head, internal/store,
internal/tui). Protocol: this campaign never edits those files; the two
one-line `Options.Terrain` wirings at the plan.Build call sites in chat.go are
handed to that session as a patch. Merges happen after the chat session
commits; rebase onto its commits, never force, `make check` before every
merge.
