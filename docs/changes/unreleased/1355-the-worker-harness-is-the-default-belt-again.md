---
kind: changed
title: the worker harness is the default belt again, and a run has no worker bound
pr: 1355
surface: [engine, chat]
invalidates:
  - "#1340 made the node belt the default again, with the exact word `bash` as the only way onto the harness. It is the other way round once more: unset `CODEAF_TASK_BELT` is the harness, and `node`, `legacy` and `off` are the words that reach the older engine. Anything else, a blank included, is the harness."
  - "A task the chat put on the harness ran its parts ONE AT A TIME whatever `task.parallel` said, because the run engine read the setting's 0 as 1. It reads 0 as no bound now, which is what the setting has always promised."
  - "`codeaf do` started four workers at once and read no setting. It reads `task.parallel` like the chat door, and `--slots <n>` names a figure for one run; there is no constant of four anywhere."
---
#1340 sent the default back to the node belt on a comparison whose cells were
launched in two unequal waves, and its own entry said the quality gap was one
task. The comparison that answers it is a 32-cell shuffled single batch on
Spark: eight DeepSWE tasks, two replicates, both belts interleaved so they saw
the same load, one model on every seat, each cell graded by its fixture's own
suite with the official test patch applied. The harness passed 3 of 14 scored
cells at a median `$0.239` and 1010 seconds; the node belt passed 2 of 13 at
`$0.384` and 2924 seconds. Pooled over every scored cell of the week, 61 of
them, the two belts pass at the same rate and the harness is cheaper. The wall
gap is the node belt running its parts one after another.

So the default is the harness, as #1335 had it, and every line #1340 changed
goes back: the predicate, the two `TestMain` pins that name the older engine's
suite once, the manual's three pages and the truth table in
`bashbelt_default_test.go`.

**The bound is gone too.** `task.parallel` is 0 out of the box and its hint says
0 is no limit. The chat door handed that 0 to the run engine, and the engine's
supervisor read a count below one as one, so a conversation's harness task ran
one part at a time while the setting beside it promised no limit. `codeaf do`
never asked the setting and carried a constant of four. Both roads read the one
row now and a 0 is no bound; `codeaf do --slots <n>` names a figure for one
run, where `0` is no limit and blank is the setting. The manual says so under
*How many tasks run at once* and in the `codeaf do` flag table.

**The tmux suite names its belt now.** Every scenario `start` launches says
`CODEAF_TASK_BELT=node`, the road it was written for, and the launcher drops the
runner's own value of the variable before the child starts, so a word exported
in a developer's shell cannot choose what the suite tests. Before this the suite
relied on the absence of a word, which is exactly the instrument fault that
would have had a benchmark comparing the harness to itself. One new subtest,
`TaskOnTheDefaultBelt`, launches with the variable absent and reads the run
road off the screen, which is the only test of the default there has ever been.

The one piece of machinery that had to move for this: the supervisor's drain
reads every outstanding return before it waits for the worker goroutines. With
no bound there can be more workers out than the return channel is deep, and a
worker blocked on handing in its return never reaches the wait.
