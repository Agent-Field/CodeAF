---
kind: fixed
title: what a goal names is measured against one worker's reach, and no pass hands over more than that
pr: 425
surface: [engine]
invalidates:
  - "Context pressure used to be a judgment: the planner's spine was asked whether the work was too large for one worker to hold, and the sizing pass was asked whether a node could be brought to an end inside what one worker holds. Both are now told. The files a goal names by name are stat'ed once at build start, weighed against the window the worker will actually get, and the measurement is stated in the shared prompt block as a fact — the spine's third gate says in as many words that it is given the figures and must not guess at size."
  - "A one-stage spine sample used to win the medoid on the strength of two samples agreeing. Where the measurement says the named material is larger than one worker's reach, a one-stage sample is now set aside before the vote, because one stage is one worker holding all of it. If every sample says one stage they are all kept: the spine is never made to invent a gate."
  - "An atomic verdict from the sizing pass used to be final. A work node whose named sources outweigh one worker's reach is now corrected to oversized and journaled as \"its named material exceeds what one worker holds\" — a new refusal constant, plan.RefusalBeyondReach."
  - "The undivided shortcut used to hand the whole goal to one fresh worker on the spine's word alone, and collapseAtomicChain used to fold two to four atomic links into one node guarded only by the link count. Both now also require that the named material fit inside one worker's reach."
  - "internal/exec.observationWindow was where the worker's byte reach was derived. The derivation is now ctxbudget.ObservationBytes and exec delegates to it: the planner asks the same question before any worker exists, and a number two packages both need has one owner. Every value is unchanged."
  - "plan.Options and plan.Graph carry a Workspace — the directory the terrain was drawn from — because a rendered terrain can be read and not weighed. A caller that sets no workspace measures nothing, renders no prompt bytes, and gets every verdict it always got."
---

Measured on the brief in #384, a plan that named three lanes of work over one
3,500-line file came back as ONE leaf, which then exhausted its context reading
its own subject; the replan after the exhaustion drew the same one leaf again.
Nothing in the run was wrong about the work — everything in it was wrong about a
number nobody had read. So it is read, once, and the passes that hand work to a
single worker are bound by it rather than by an opinion about it. A goal that
names no file that exists is not treated as small: it is treated as unmeasured,
renders nothing, and changes nothing.
