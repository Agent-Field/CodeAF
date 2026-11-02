---
kind: fixed
title: codeaf desktop saving helpers expire after quit
pr: 2226
surface: [engine]
invalidates:
  - "Desktop saving helpers used the same ten-minute idle limit as terminal helpers. Helpers the desktop engine starts now use one minute, with health requests keeping them ready while the engine is alive."
---

After the desktop engine quits or is killed, its saving helpers exit after about
one minute without activity. Quit sends no shutdown request; another window can
keep using the helper. Health requests run about every 20 seconds, ignore errors,
and never start a helper. Existing helpers keep their limit, and terminal and
command-line helper lifetimes are unchanged.
