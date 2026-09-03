---
kind: fixed
title: a file the request names is never scratch, so a leaf changing it is never at a standstill
pr: 416
surface: [resident]
invalidates:
  - "A job's focus was what its request names RESOLVED against the workspace, and only entries that came back holding a path separator were kept — so a file the request wrote out in full at the top of the workspace was dropped from the focus. It is now the union of the paths the request spells that the workspace holds (one os.Stat each, no walk) and that resolution, so a change to a request-named file is Relevant wherever it sits."
  - "A round that edited the one file its brief named could be journaled `produced: 0` with that file under `wrote`, and two of those were refused with 'carrying on has stopped changing anything'. It now records produced ≥ 1 and is never refused as a standstill."
  - "focusShape filed a focus entry at the top of the workspace under the empty directory, which would have made every root-level file 'beside' it. The root is not a directory a focus can be in and is no longer recorded as one."
---

The growth governor's standstill rule is the only rule that asks whether a round
ACHIEVED anything, and it was reading the wrong answer for the commonest shape of
job there is: a request written around one file, named in full, sitting at the top
of the workspace. The measurement said that file was scratch, `produced` was zero,
and the second such round handed the work over while it was moving. What the round
produced is now judged against what the request names, before what the plan chose
to list.
