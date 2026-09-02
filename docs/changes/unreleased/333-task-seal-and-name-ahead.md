---
kind: fixed
title: a task on an attached repository seals, and work aforge starts is named before it is announced
pr: 333
surface: [engine, chat]
invalidates:
  - "The snapshot rung's seal staged with `git add -A -- . ':(exclude).aforge-v3' ':(exclude).furrow'`, and the fork rung's with the first exclude alone. Git reads an exclude pathspec that literally names an ignored path as an attempt to add it and exits 1 after doing the work — and `hideFurrowMarker` hides `.furrow/` in `.git/info/exclude` the moment furrow attaches a folder, so every task grounded on an attached repository failed in its first second with \"could not prepare a working copy: this task's world could not be sealed: The following paths are ignored by one of your .gitignore files\". Both rungs now stage with a plain `git add -A -- .` and take the corners back out with `git reset -q -- .aforge-v3 .furrow`, the way `stageTaskWork` always has."
  - "The wire ceiling for a model that runs a thinking pass nobody asked for was the share formula alone: the answer's size divided by what the model's own level leaves. Off the namer's 32-token answer that was 160 tokens, the model thought through all of them, and the memo already knew the model so nothing asked again. A pass the caller never asked for is now given at least `unaskedThinkingFloor` (2048) tokens of room, whatever the answer's size; a word the caller chose keeps the router's share, and large answers are unchanged."
  - "The task namer started at admission, after the handover's brief was written, so the `this looked like work, so task N started:` line and the first row on the rail carried the person's raw sentence and were renamed a moment later — or never, when the task failed first. Both roads that start work on their own (the judge's and the ceiling's handover) now ask for the name the moment they decide to (`nameAhead`), beside the brief-writing; a name in hand at admission is the title from the first line a person reads, one still in flight rides `taskSpec.ahead` and `nameNode` waits on it rather than asking again, and a road that declines cancels the call. `launchRouteTask` takes the name as a fifth argument."
  - "The route test double `routeCompleter` answered every belt-less errand, the namer included, with `(errand)`. It now answers the namer with nothing, because a placeholder that lands before the task is announced becomes its name."
---

One run, three faults met in order: the task failed in its first second, under the URL it was cut from, and would have kept that URL as its name had it lived.
