---
kind: renamed
title: a flag about money is not spelled like a flag about tokens, and a duration flag takes a duration
pr: 518
surface: [build, docs]
invalidates:
  - "`--budget` meant TOKENS on `exec` and `run` while `--budget` is a word about money everywhere else in this product (`AFORGE_DAILY_BUDGET`, `/budget` in the chat, `--max-cost`), so `exec --budget 150000` read as $150,000. The token wall is `--token-budget`, and `run --run-budget` is `--total-token-budget`."
  - "`--turns` is `--max-turns`, `--ensemble N` is `--passes N`, and `--brief` on the planner is `--instructions`."
  - "`wake --max-seconds` was the third spelling of a wall the other verbs already had. It is `--timeout`, which takes a duration — `--timeout 15m`, `--timeout 2h` — with bare seconds still accepted for one release."
  - "Every one of those old spellings still works for one more release, is absent from `--help`, and prints one line on stderr the first time it is used. After that release they are gone."
  - "`-w`, `-o` and `-j` are NOT deprecated and are not going anywhere. They are shorthands for `--dir`, `--out` and `--parallel`, hidden from `--help` because the long name is the printed one, and they say nothing when used. Before this, `-w` had no long form at all, so per-command help printed it as `--w` while the usage table printed `-w` and a reader could not tell which was real."
---

The two kinds of hidden flag have different lifetimes and are different functions
at the call site for exactly that reason — `renamedFlag` is an old spelling on a
one-release clock, `shorthandFlag` is a single letter that keeps working forever.
