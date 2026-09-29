---
kind: added
title: bench/frontiercode — the five remaining pilot candidates are built and calibrated
pr: 1700
surface: [build, docs]
invalidates:
  - "The pilot corpus was two calibrated tasks (jsonschema-log-warning, conflicted-files-refname-crash) with five candidates still sketches in PLAN-pilot.md. All seven candidate tasks are now built and calibrated — shell-pipe-empty-command (goreleaser #5929), subcommand-single-char-alias-leak (kong #637), completions-args-mutation (cobra #2356), dns-extra-records-lowercase (headscale #3366), progress-state-string-out-of-range (bubbletea #1748) and mangen-hidden-positionals (clap #6482) — and shards/shard-1.txt lists all eight built tasks, its hash re-pinned in the pilot manifest. Every candidate reproduces both controls locally: gold 1.00, negative 0.0 with both blockers failed."
  - "A candidate reference patch spanning a multi-commit PR is a base..head diff, not format-patch -1; the head-only patch does not apply to the base for kong #637, cobra #2356, bubbletea #1748 and clap #6482."
  - "A rubric scope criterion with allowed_paths [\"./\"] matches no file under the rig prefix matcher; tasks name real paths (e.g. [\"tea.go\",\"tea_test.go\"]) instead."
---

Five candidate task environments were built to make the pilot corpus real before
any campaign runs at scale, following the two calibrated tasks exactly:
task.toml with agent_repo_path set, environment and tests Dockerfiles, a
labelled-negative patch, an instruction and a rubric-schema-v1 rubric with at
least two blockers. Base SHAs and PR diffs were verified against GitHub via
curl (git remote is refused in the sandbox). Image sizes for cost planning:
the Go tasks are comparable to the built fixtures; clap (Rust) is the outlier
at ~2.36 GB env / ~2.46 GB verify images.
