---
kind: fixed
title: a handoff turn says what did not start, and the reply checks are no longer chat rows
pr: 2838
surface: [engine, chat, app]
invalidates:
  - "Was believed: a task proposed with depends_on naming the task already running joins that run and waits for it. Is true: a joined task is a child of the run's root, and when the root IS that task the store refuses the edge ('has hard dependency across its containment lineage'), so the task never started (qloop run 16, vector-db t1). Now such a proposal is refused before a card with 'depends_on names task N, which is still running … Propose this again when task N's report arrives'."
  - "Was believed: under the bash belt, depends_on may only name a task in the run now turning, and anything else is 'no task in this session has that id'. Is true: a task from a run that has ended is read off its row; ended done is a dependency met and dropped, ended any other way is refused as 'ended without finishing'."
  - "Was believed: a turn may end on 'I'll report when X lands' whatever happened to X. Is true: a hand-off that did not start (and was not started again under the same title) sends the reply back once to start it or say so, and a promise of a later report with nothing of this conversation still running sends it back once to finish or say what is left. Both are asked before the look check."
  - "Was believed: the #2836 check lines ('it changed the work after its last look · asking it to look again before it replies', the contradicted-look line, the failed-steps line) are notices every surface draws. Is true: they are carry-on lines (IsCarryOnNotice); the app draws no row for them and the terminal folds them with the turn's work. Each send-back writes a 'sent-back' rule line in the session journal."
---
