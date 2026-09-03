---
kind: fixed
title: the planner is told what a goal names against one worker's reach, and no pass hands over more
pr: 425
surface: [engine]
invalidates:
  - "the planner's reach measurement lives on this branch — no longer true. Its core is on `dev` via #564: `Reach`, `Measurement`, `Reach.Measure` and the share reader that weighs the material a source scopes rather than the file it names, `correctBeyondReach` storing the one per-node verdict on `Node.BeyondReach`, `JudgeSplit` reading that field, the division exemption, and `ctxbudget.ObservationBytes`. This PR is the other half: the spine's admissibility over the GOAL's measurement, and the prompt line that carries it."
  - "context pressure was a judgment the spine was asked to make: gate 3 asked whether \"the work is too large for one worker to hold in its context at once\". It is now measured and stated. `Measurement.Line` renders into the shared goal block every planning pass already sends — grounding, the spine, the panel decision and the whole-graph passes — and gate 3 says in as many words that the figures are given and that context pressure is not a reason for a stage where they are absent."
  - "a one-stage spine sample used to win the medoid on the strength of two samples agreeing. Where the goal's named material is larger than one worker's reach, a one-stage sample is now set aside before the vote, because one stage is one worker holding all of it. If every sample says one stage they are all kept: the spine is never made to invent a gate. `Spine` and `spineWithProgress` take the measurement; `GroundWith` and `DecidePanel` take its rendered line."
  - "the undivided shortcut used to put its one folded node to the ruler on every road. Where the goal's material has been weighed larger than one worker's reach, the ruler is not asked at all — the arithmetic has already answered its one question — and the build falls through to the full pipeline, reported as `measured past one worker's reach — planned in full`. That shortcut is the road a remainder takes after a worker ran out of context, which is where the same exhaustion was measured being bought twice (#384)."
  - "`collapseAtomicChain` used to fold two to four atomic links into one node guarded only by the link count. The folded node carries the whole goal as its brief, so it now also requires that what the GOAL names fits inside one worker's reach. It deliberately does not weigh the links' own sources: a node's verdict has one computation, `correctBeyondReach`, and a reader here cannot see the siblings its exemption turns on."
  - "`plan.Graph` carries a `Named` — the goal's measurement rendered once at build start and frozen for the build, beside the terrain and for the same reason. It is the whole-plan reading and never a node's."
---

Measured on the brief in #384, a plan that named three lanes of work over one
3,500-line file came back as ONE leaf, which then exhausted its context reading
its own subject; the replan after the exhaustion drew the same one leaf again.
Nothing in the run was wrong about the work — everything in it was wrong about a
number nobody had read.

#564 landed the reading itself and the per-node verdict computed from it. This
is the half that stands in front of that: the number is stated to the passes
that decide the SHAPE of a build, and the three roads that hand a whole goal to
one worker are bound by it. Each of them reads the goal and none of them reads a
node — a goal that names more than one worker holds is a true statement about
the whole plan, while a node is weighed against its siblings, once, in the one
place that can see them.

A goal that names no file that exists is not treated as small: it is treated as
unmeasured, renders nothing, and changes nothing. A run with no workspace sends
the prompt bytes it always sent and takes every road it always took.
