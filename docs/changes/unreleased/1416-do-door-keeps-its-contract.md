---
kind: fixed
title: codeaf do on the run engine edits in place, commits nothing, and stops at a price
pr: 1416
surface: [engine, chat]
invalidates:
  - "`codeaf do` on the run engine committed the directory's whole `git status` as `task: <title>` on the checked-out branch, the person's own uncommitted edits and untracked files included. It edits the directory in place and commits nothing, as `--dir` says, and its files are only the ones the run changed."
  - "A `codeaf do` run on the run engine had no spending bound and `--yes-spend` did nothing. Without the flag it stops at the plan price (`CODEAF_PLAN_CONSENT`) or at what is left of today's limit, with exit 3; `--yes-spend` or `CODEAF_PREAUTHORIZE_SPEND=1` lets it spend past both."
  - "`codeaf do --db` and `--keep` were silently ignored on the run engine. `--db` is refused with exit 1 and a sentence naming the older engine, and `--keep` says where the run's store is."
  - "A belt landing left out every path ending in `.lock`, so a run's change to `yarn.lock`, `Cargo.lock`, `poetry.lock` or `flake.lock` never landed, and a `bench-results/` folder never did either. Only the paths the harness itself writes are left out now."
  - "`CODEAF_CHECK_MODEL` seated checks for `codeaf do` only. A chat `/task` run reads it too."
  - "An approved hand-off the run engine could not start fell back to the older engine with a receipt identical to the run road's. The receipt now says it runs on the older task engine and why."
  - "The v0.4.0 changelog's #1109 entry says the node engine is deleted, that `CODEAF_TASK_BELT` does not exist, and that `propose_task`, `quick_task`, `divide_work` and `revise_assignment` are gone. None of that is true: the node engine is in the binary beside the run engine, `CODEAF_TASK_BELT` switches between them (unset is the run engine; `node`, `legacy` and `off` reach the node engine, see #1355), and those four tools are still the node engine's doors."
---
The run engine became the road every `codeaf do` takes (#1355), and it kept
none of the older road's promises about the directory or the money. The older
road edits in place and never commits, and the help for `--dir` says exactly
that, so the run road now keeps the same contract: the folder is read before
the run starts and afterwards, and the files the run names are the ones it
changed. The plan-price question cannot be asked before a run starts, because
nothing prices a run up front, so the same figure is a ceiling instead.
