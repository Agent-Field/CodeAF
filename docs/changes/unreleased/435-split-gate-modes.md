---
kind: changed
title: the split gate's reading is a pin now, so the experiment can ask each one
pr: 435
surface: [engine]
invalidates:
  - "AFORGE_SPLITGATE is a switch with two positions, on and 0 — no longer true: it names which reading of the gate the binary runs. Unset and `1` are the shipped counting; `0` is the same rollback it always was; `lanes` counts the shapes a division is written down in as well, and takes a division written out as labelled lanes at its word with no floor applied; `judgment` asks the plan's own sizing instead of the brief. Anything unrecognised reads as the shipped counting."
  - "the six-item floor is the last word on every reading the gate makes — no longer true under `AFORGE_SPLITGATE=lanes`: a brief that names its parts as parts is kept whatever it counts, because the floor was measured on counts of items and not on divisions somebody wrote out. Every other reading, in every mode, is still weighed against it."
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
rollback switch, and one binary runs any of them. `lanes` reads a numbered or bulleted
list, a spelled-out number beside a plural, and a list of distinct file paths as
counts against the same six-item floor — and carries one law on top of the
count: a division somebody wrote out is not a pile to be counted. Where the
brief names its parts as parts (`L1 … L3`, `lane 1 / lane 2`, `part A / part B`)
the division stands with no floor applied, because the floor was calibrated on
counts of items and a person naming lanes has already answered the question it
exists to ask. A plain list marker is not a label — the bench corpus numbers its
three bugs and its four modules down the page, and those two are the measurement
that three and four do not pay. `judgment` keeps a division
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
