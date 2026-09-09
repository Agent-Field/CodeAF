---
kind: fixed
title: a landing that is your call can be answered from a window on an engine host
pr: 743
surface: [chat, remote, docs]
invalidates:
  - "A landing card that came home as `your call` drew its reason and NO ANSWERS ROW on an ordinary launch. It was not the layout and not the fixture: v3's normal road talks to an engine host over internal/remote, whose agent had none of the four doors that decide a landing, so the surface's type assertion failed and the absence law removed the whole row. internal/remote now carries `Task.Settle` and a `Welcome.TaskSettle` flag, and the card draws `[a] accept · [n] not right · [s] tell it · [d] let aforge decide this one` over a connection exactly as it does in process. An engine older than this build still draws the reason and no chips."
  - "`app.settleDoors()` and `app.conflictDoors()` used to be a bare type assertion. They now also ask an optional `SettleSupported()` — a type assertion cannot see across a wire, because every remote agent has every method whatever the machine at the far end is."
  - "docs/design/task-states/DESIGN.md said a done row carries `branch kept` ONLY WHEN KEEPING WAS ASKED FOR. That was wrong: the fact appears whenever the landing kept the branch, and asking for it is one of four ways — the other three are a protected checkout, a detached one, and a branch that moved since the work was cut. The card's own report says which."
  - "internal/e2e's `TestTaskStatesE2E/the_rail_and_the_roster_say_the_same_word` used to skip naming #707, and `testStatesConflict` used to skip because the landing kept its branch. Both now run. The rail half was a misreading, not a defect: `statesRailRow` could never match a column row, so it pressed ctrl+g, closed the column it was measuring and read the tab strip instead. The column has been saying `your call · nobody could check it` under the name all along."
  - "A test that wants a real merge conflict may not COMMIT the person's clashing change while the work is out: a commit moves the branch, and a branch that moved since the cut is one aforge keeps rather than writes. It must leave the change uncommitted, on a checkout that is not on a protected name."
---

The measured shape was two identical seeded landings in one binary, one drawing four
chips and one drawing none. The difference was the length of the temporary home each
subtest ran in: a host is reachable only where its socket path fits under a hundred and
four bytes, so the shorter-named subtest got an engine host and the longer one fell back
to the in-process engine — and only the in-process engine had ever been able to answer a
landing.
