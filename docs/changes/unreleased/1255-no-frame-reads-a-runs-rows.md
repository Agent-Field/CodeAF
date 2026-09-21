---
kind: fixed
title: drawing the side list never waits on the engine for a run's rows
pr: 1255
surface: [chat]
invalidates:
  - "The side list's rows of a run were read inside the freshness check every drawing path comes through (`tasksPlace.regroup`, by way of `taskSheetMine`), so a FRAME asked the engine for them. Three things made that read due and each made it from a frame: the first reading of a conversation, a row of this window's own graph moving, and the run's three second beat. Over a connection the read is a call to another process with a ten second deadline. With 250 ms injected on that one read, the frame that made it took 254 to 258 ms on all three; it now takes under 1 ms and makes no read."
  - "The rows are held on the surface (`app.planRows`) and asked for after a message (`app.refreshPlanRows`), one read at a time and beside the ordered line. `tasksPlace.planDue` and `planReadAt` are gone: the beat lives with the read, and `regroup` re-files what is held when a newer read has been folded in (`planRowsGen`). What makes a read due is unchanged, and a conversation at rest still reads nothing."
  - "Opening the run's tab read the rows and the run's page on the key press itself. It opens on the held rows and asks for the page through the one door every stored page arrives through, off the loop; `openWorkTab` returns that command."
  - "The law that keeps engine doors off the update loop could not see a door reached through `planReader`, so none of this was ever reported. It now counts that reading as handing the agent back, which puts every plan door under the law."
---

An earlier change moved the beat's read off the frame and left the other two. A
test that calls `regroup` and expects fresh rows now folds the read first, the
way a window does after a message (`readPlanRows` in the tests).
