---
kind: changed
title: the split gate's reading is a pin now, so the experiment can ask each one
pr: 435
surface: [engine]
invalidates:
  - "AFORGE_SPLITGATE is a switch with two positions, on and 0 — no longer true: it names which reading of the gate the binary runs. Unset and `1` are the shipped counting; `0` is the same rollback it always was; `lanes` counts the shapes a division is written down in as well; `judgment` asks the plan's own sizing instead of the brief. Anything unrecognised reads as the shipped counting."
  - "the split gate's answer is internal/splitgate.WorthIt over the text — no longer the whole of it: both doors now ask splitgate.Judge, and the plan door hands it the planned leaves (their size and what they wait on) because a mode may prefer the plan's sizing to anything the brief said. A caller with no plan passes nil and gets the count."
  - "internal/splitgate.Items is what a refusal says back — no longer true: the session's `not split` sentence and the folded node's own line both read splitgate.Count, which is the current mode's counting. Under the unset pin it is Items, byte for byte."
---

The gate folds a real division because its reading of the brief is a count of
digits standing beside plurals, so a brief that names three lanes over one file
counts zero and runs as one over-large sitting (#418, found by the autopsy on
#252). Two repairs were proposed and neither was argued to a conclusion — count
better, or stop counting and ask the plan's own sizing — and the owner ruled
that the choice is made by a designed experiment, planner arm against gate mode,
rather than by whichever repair somebody wrote down first.

So both live here at once, behind the pin the gate has always used as its
rollback switch, and one binary runs any of them. `lanes` reads labelled lanes,
a numbered or bulleted list, a spelled-out number beside a plural, and a list of
distinct file paths; it moves the counting and not the six-item floor, by
design, so a brief naming three lanes still folds. `judgment` keeps a division
whose work leaves are each sized a sitting and owe each other nothing, whatever
the brief counted, and falls back to the count everywhere else — an unsized
leaf, a leaf still oversized, or any edge between two leaves — so it can only
ever add keeps to what the shipped gate would have done. That one-directionality
is what makes it readable as a factor rather than as a different gate.

The selector lives in the package and not in either caller, because the law that
put the counting there in the first place is that one decision may not have two
implementations. Nothing moves with the pin unset: the same count, the same
floor, the same yes, and the same sentence written into a folded node.

This is scaffolding with a known end. When the experiment names a winner, that
mode becomes the gate, the selector is deleted, and `AFORGE_SPLITGATE` goes back
to meaning nothing but `0`.
