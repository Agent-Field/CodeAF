---
kind: changed
title: bench/frontiercode — the pilot protocol aligned with the official FrontierCode specification
pr: 1700
surface: [build, docs]
invalidates:
  - "A campaign ran one trial at one pinned reasoning effort. Seeds and reasoning effort are now the two swept dimensions of a campaign: the manifest lists `seed_ids` (default 5 — the official protocol runs 5 trials per model per reasoning-effort level) and `reasoning_efforts`, launch.sh enumerates every effort × seed × wave with an iteration label per cell, run directories and container names carry the level (`-e<effort>`) so two levels or two trials never collide, and grade/report.py prints a per-level aggregate — mean score, mean output tokens, pass count — and names the best-performing reasoning effort, as the official protocol reports. A one-element list pins a single trial, so a canary still runs one rollout."
  - "The report carried output tokens per run only. The per-level aggregate now reports the mean output tokens per rollout beside the mean score and pass rate — pass rate and weighted score (blockers gate to 0) were already both computed per run."
  - "The rig's relationship to the official benchmark was folklore. README.md §13 \"Conformance with FrontierCode\" now maps each official requirement (cited to https://cognition.com/blog/frontiercode — read 2026-06-08; a re-fetch on 2026-09-29 returned 404, so the citation names the capture the table rests on) to conformant / adapted / not-closable with the reason: the mergeability endpoint, both metrics and the scoring rule are conformant; the grading ensemble, uniform rubric weights and the egress posture are adapted; task provenance, task count and subsets, the QC pipeline and rubric authorship are not-closable and are named as such rather than glossed. GCP.md's manifest table documents the new fields and references that section."
---
