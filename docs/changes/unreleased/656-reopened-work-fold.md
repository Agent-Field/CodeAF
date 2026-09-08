---
kind: fixed
title: reopening long conversations keeps completed work folded
pr: 656
surface: [chat, engine, docs]
invalidates:
  - "Replayed work was believed to derive the same closed chip as completed live work. The leading fragment of a long turn is numbered zero, and zero also meant no running turn; comparing them kept old captions and calls visible. Only a nonzero running turn now prevents folding, including after older history is loaded."
  - "The checkpoint continuation was recorded as an unmarked user message and appeared after reopening as a question nobody typed. Its full reserved lead now identifies private model context in existing journals as well as new ones; it stays in the model's record but is omitted from the displayed exchange."
  - "Display shaping omitted volatile notes while compaction's entry count still included them. Both now use the same exclusion rule for private context, so the count and the displayed history agree."
---

The regression fixtures cover a turn longer than the replay window, repeated
backfill, opening its chip and calls, and reopening an older unmarked continuation
from a real journal. Completed conversations retain the question and final answer;
`ui.work = open` remains the explicit preference for expanded work.
