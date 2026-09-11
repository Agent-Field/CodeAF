---
kind: added
title: the prompt diet is measured against dev, not asserted — a four-layer parity bench
pr: 812
surface: [docs, build]
invalidates:
  - "The prefix budget test was the only number a prompt change could quote, and it was quoted as though it settled something. It does not: it weighs the rendered page and the marshalled belt and says nothing at all about whether the build still behaves. `bench/prompt-diet/run.sh <branch> <label>` measures a build in four layers — the budget test's exact page/tool-block split, the tagged Go suites that drive the real binary in a real terminal against a real model, the `bench/conversation` and `bench/e2e` cells handed that binary, and a per-request wire ledger — and `bench/prompt-diet/compare.py <baseline> <candidate>` rules on the two claims separately: PARITY, meaning every subtest and every cell ends equal or better, and EFFICIENCY, meaning fewer prompt tokens per TURN. It exits non-zero when an outcome got worse."
  - "`bench/` was believed to hold nothing that compares two branches of aforge. It still holds nothing that does — `bench/e2e compare` is run-versus-run, `bench/swarm/ab.sh` and `bench/oneroad` are binary-versus-binary — and `bench/prompt-diet` does not add a fifth cell format either. It is an ORCHESTRATOR over the batteries that exist: the rig is the checkout the script lives in and the subject is only the built binary, because `dev` at 6aa6a946e has never heard of this directory and a rig that moved with the subject would compare two harnesses rather than two prompts."
  - "Seeing every model call was thought to need a proxy somebody had to write, at AFORGE_BASE_URL. It does not, and none was written. `bench/conversation/lib/guard.py` is already a loopback forwarder every cell runs in front of OpenRouter as AFORGE_BASE_URL, already writing prompt, cached, completion and reasoning tokens with a cost per request; `internal/calllog` under AFORGE_CALL_LOG_BODIES is the only place a request's tool-block bytes can be counted rather than inferred. `lib/wire.py` normalises both and keeps `source` on every row so they are never added together."
  - "The bench brief asked for cells covering a conversation turn, a task handoff, a question, a standing item and a media refusal. Three exist; two do not, and docs/design/prompt-diet/BENCH.md §2 says so rather than implying coverage. There is no question cell anywhere under `bench/` — every arm of every battery runs unattended, so consent is bypassed by construction and there is no assertion vocabulary for an agent-to-person `ask`. There is no standing cell. A MEDIA REFUSAL is covered by nothing at all, which matters because lane E moves the 853-byte media essay off the page and nothing in the tree would notice if that changed what the chat says when somebody asks for a picture."
  - "Nothing under `internal/` moves in this change, so the prefix budget on this branch is unchanged at 47,435 bytes (prompt 23,391 + tools 24,044) against a cap of 48,000. That figure agrees to the byte with the audit table in docs/design/prompt-diet/DESIGN.md §0, which is the first thing the bench proves: the audit was measuring the build the wave is about to change."
---

The wave's acceptance is the owner's own sentence — "real e2e with dev and our
branch, same other changes, just this diff alone, and see if we are on parity but
efficient" — and it is two claims that can disagree. A build that answers in
three rounds where the baseline took six has a smaller bill and a *bigger*
per-turn figure, so efficiency is measured per turn and the round count is
printed beside it. Wall clock and dollars are recorded and never ruled on:
`bench/e2e/README.md` measured a 60% wall swing between two runs of identical
code, and a battery that fails on a healthy run stops being read.

`docs/design/prompt-diet/BENCH.md` carries the baseline, the ssh recipes, the
frontier arm, and the ablation — prepared and not run, with the two of its ten
largest law units that cannot be reached at all until a lane adds
`AFORGE_PROMPT_ABLATE` beside the law registry.
