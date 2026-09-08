---
kind: renamed
title: the four pipeline verbs live under `aforge plan`, and `aforge run <name>` is the saved program
pr: 518
surface: [build, docs]
invalidates:
  - "`aforge run` named two unrelated commands: a saved program (`aforge run subharness <name>`) and the static graph pipeline (`aforge run <plan.json>`). The proof it was overloaded was in the code — a `longerCommands` table existed for no reason but to stop `aforge run --help` printing the wrong one of the two. The saved program is `aforge run <name>` now, and the pipeline is `aforge plan run <plan.json>`."
  - "`aforge show <plan.json>` and `aforge revise <plan.json>` were top-level verbs. They are `aforge plan show` and `aforge plan revise` — four verbs under the noun they all act on."
  - "`aforge plan \"<goal>\"` was the whole command. It is `aforge plan new \"<goal>\"`. A lone `new`, `show`, `revise` or `run` in the first position is read as a subcommand and anything else is read as the goal, which is the reading `aforge cache clean` already had."
  - "Every one of those five old spellings still works for one more release. It runs exactly as it did, is absent from `--help`, and prints one line on STDERR the first time it is used: ``note: `aforge show <plan.json>` is now `aforge plan show <plan.json>` — the old spelling works for one more release.`` After that release they are gone."
---

The notice is on stderr and nowhere else, because stdout carries the answer — the
deliverable, the rows, the `--json` object — and a deprecation sentence printed
in front of an object somebody is piping into `jq` would break the very callers
the grace period exists to protect. Asking an old spelling for `--help` prints
the NEW spelling's line and says nothing: the notice is about a run, and there is
no run.
