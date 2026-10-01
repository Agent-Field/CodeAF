---
kind: fixed
title: A shell senior-dev run with flags after the brief runs instead of failing to parse
pr: 1717
surface: [engine]
invalidates:
  - "#1700's entry said flags after the brief are parsed wherever they sit for every delegate program. On the shell road in a repository, `codeaf senior-dev \"<brief>\" --max-cost 0.5` then announced $0.50 and died: the copy's note was put between `--max-cost` and its value and the child refused to parse the line. The invocation's line is now kept flags-first, the way the parser reads it, so the child gets the same ceiling, the copy's folder and the note ahead of the brief in any typed order."
---
