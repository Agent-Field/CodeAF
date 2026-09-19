---
kind: fixed
title: a run works in a copy of its own, and its work comes home when it ends
pr: 1232
surface: [chat, engine]
invalidates:
  - "With the bash belt on, a hand-off's workers typed in the person's own folder and the run's landing committed straight onto the branch they had checked out, while the receipt said `in a copy of its own`. A run now works in a copy cut from the folder the task is about, as that folder stands, and the receipt is true."
  - "A run's work now comes home the way a task's always has: committed in the copy, merged into the folder it was cut from, the copy given back, and `its work is in <folder> on <branch>` on the run's page. Work that will not go in keeps its branch in the person's repository and the note names it."
  - "The landing card read `branch kept` for every run that named a branch. It says `merged` when the work is in the folder, and `branch kept` only for a branch that is waiting."
  - "A hand-off that joined a run already underway stayed `running` on the rail after the run ended. It ends with the run, in the state the store gives it. A proposed task about ANOTHER folder than the run underway is refused with both folders named and `Propose it again when that work has ended`; every other failure of the run road still falls through to the shipped engine."
---

Seen on the real binary on 2026-09-19, hosted, with two hand-offs typed six
seconds apart and an unsaved file of the person's own in the folder. While the
run worked the folder held only that file. When it ended the folder held one
new commit with the three files the run wrote, the unsaved file was untouched,
the copy was gone, the card read `done · 30s · 3 files · merged`, and the
joined hand-off's row had settled under the run's.

A hand-off after a run has ended starts a run of its own, in a new copy cut
from the folder as the first run left it.
