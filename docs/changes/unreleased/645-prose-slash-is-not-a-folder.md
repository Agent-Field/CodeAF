---
kind: fixed
title: a slash written as prose is not a folder, and a task is no longer refused over one
pr: 645
surface: [engine, chat]
invalidates:
  - "`looksLikePath` read any token holding a `/` as a path, a token that is nothing but separators included — no longer true. A path token must NAME something: after a leading `~` comes off and the spelling is cleaned, a separator root and the two relative roots are prose, so `pathTokens` yields nothing for ` / `, `and / or`, `//` or a bare `~/` and still yields `/etc/hosts` and `~/project/file.go` from the same sentence."
  - "A proposal whose acceptance used a slash as ordinary prose — \"the header renders / no regression\" — was refused with `this task names a folder it does not stand in: /` and admitted no node; it is admitted now. The refusal fired on two of six real-model attempts and only where `where` was empty, because a filled `where` answers at rung `named` or `here` and the ground lint never runs."
  - "The fix is at the recogniser and not at the refusal, so every reader of `pathTokens` moved together: the reground walk, the two-roots question, the deliverable refusal, `groundNamesWorkUnder`, `placeNamedIn` and `task_divide_scope.go`'s `scopeCollisions`. A prose slash shared by two parts was never a scope collision and is now not a path either."
  - "Both refusal sentences are unchanged. A contract naming a real absolute path outside its ground, in no repository, is still turned back with `this task names a folder it does not stand in: <path>`, and two such repositories still ask `this task names folders it does not stand in: <a>, <b>`."
---

The chat manual's ground page named the refusal and never said what makes a word
a path at all, which is the question somebody has the moment they read `this task
names a folder it does not stand in: /`. It says it now, beside the refusal it
qualifies rather than at the end of a section already past `SectionBodyCap`, so
the model is handed the whole clause instead of half of one.
