---
kind: removed
title: There is one worker — swe, the bare worker and every way of choosing between workers are gone
pr: 235
surface: [engine, chat, build, docs]
invalidates:
  - "aforge shipped three leaf workers — `linear`, `swe` (a vendored end-to-end coding engine under `internal/swepro`) and `bare` (a pi-shaped one-sitting loop). It ships one: `linear` takes every leaf, in chat and in `aforge do`."
  - "A model chose a worker in four places — the compiler's menu, the sizing pass's specialist section and schema, the retry-worker judge after a failed attempt, and the recalibrate preamble for a specialist's ruler. None of those prompts exists; the compiler, sizing and recalibrate prompts are the bytes their goldens pin, unconditionally."
  - "A failed leaf attempt could be escalated to another worker (`escalated linear → swe: escalated from linear after a failed attempt`), an exhausted continuation climbed a ladder (bare → linear → the job's specialist), and a straggling leaf could be handed back to a judge that re-chose its worker. A failed attempt now retries on the same worker on a stronger model or ends with that failure named; no `node_worker_changed` is ever written again, though an old journal that carries one still replays and still narrates."
  - "`aforge do --subharness <name>` and `aforge run --subharness <name>` pinned (and, per #207, did not really pin) a worker. The flag does not exist; `flag provided but not defined`. `aforge run subharness <name>` — the saved-program door — is unchanged."
  - "The `work.workers` setting (`AFORGE_WORKERS`), its row on the settings sheet and on the Tasks tab, filtered which workers an install could hand work to. There is nothing to filter; the key is ignored if a profile still carries it."
  - "`AFORGE_SWE_MAX_COST` capped one swe leaf's spend and `AFORGE_SWEPRO=1` re-executed the binary as the engine. Neither variable is read; `aforge doctor` no longer has a coding-model row."
  - "`/subharness` and `list_subharnesses` listed `swe` and `bare` beside the saved programs, because every registered leaf worker was fronted as a program. Only saved programs are listed; the generalist is what you get when you pick nothing and is never on the list."
  - "The load governor admitted local work (only swe ever was) against the host's load average with a 1.5/1.2 hysteresis; that half is gone and admission is in-flight count alone. The manual's money-and-limits page said load was part of admission; it says it is not."
  - "The sizing pass routed atomic no-input nodes to `bare` by default and refused to split a node a specialist had been named for. Every work node is linear and splits on its own size alone."
  - "The manual's how-tasks-run page had a section 'Which workers are installed — only use the normal workers, turn off the specialist worker'; commands.md described the `workers` setting. Both are gone, and the three probe questions that reached them are gone from the manual test."
  - "`docs/SUBHARNESSES.md` (plural) described the worker registry, the ladder and swe's prompts; it is deleted. `docs/SUBHARNESS.md`, `SUBHARNESS-PRD.md` and `SUBHARNESS-CONTRACT.md` (singular) describe the saved-program contract and stay, minus the sentences that said linear and swe were re-fronted through it."
  - "`make test` excluded `internal/swepro` and `make test-swepro` ran it apart; `ci-full.yml` did the same. `make test` is `go test ./...`, the target is gone, and the only packed corpus is `internal/manual`."
  - "Bench had a `swe` mode and a `select` mode and wrote a `subharness_chosen` column; `bench/run.sh` has `do` in place of `select`, no `swe`, and no such column. `bench/oneroad/swe/` is the SWE-bench track and is untouched — it never named the worker."
  - "`SIZE-BUDGET` was 59,515,000 and had only ever risen. It is 54,600,000 — the first reset downward — two percent over darwin/amd64, the largest of the four platforms measured in PERF.md."
  - "Engine view roots and `.plandb/` worktrees that swe left under `~/.aforge` on a machine that ran it were reaped by the swe worker's own view code; nothing reaps them now, and they can be deleted by hand."
  - "A store written before this change whose node says `subharness=swe` or `bare` still opens and resumes; that node runs linear with one receipt line saying the build has one worker."
---

Three of four headless runs in the 2026-09-01 bake-off lost most of their wall
to the swe lane — `--subharness linear` was overridden 3m26s in, and the
specialist then parked in `go test` for 22 and 43 minutes with no model calls.
Bare, the other specialist, was the silent default for atomic no-input nodes
and had never been measured against linear at all. Every menu, judge, ladder,
roster and flag in the tree existed only because a second worker did; with one
worker each of them was a switch with one arm. So the decision (#227) is the
whole thing rather than the half #221 asked for: one worker, and no surface on
which a model or a person could name another. A specialist returns only as new
registry work, judged on its own measurements.
