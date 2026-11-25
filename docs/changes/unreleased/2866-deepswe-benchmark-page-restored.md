---
kind: fixed
title: The DeepSWE benchmark page is back in the repository
pr: 2866
surface: [docs]
invalidates:
  - "The README claimed first place on DeepSWE with no method behind it on dev: docs/benchmarks/deepswe from #1511, #1877 and #1913 only ever reached feat/agents-errand, and #1921 removed the README link to the missing folder. The page, arms.csv and versions.csv are now on dev, linked from the README's Benchmarks section, the documentation map and bench/README.md."
  - "The per-task DeepSWE records were said to stay on the run machines. The ten comparison tables and the two later single-arm runs (113 rows each, from 9b4344aaf on another branch) are now in docs/benchmarks/deepswe/, one folder per campaign."
---

The scores are unchanged. The page now also says where its data and its rig live, and adds the cuts codeaf placed first on: medium and hard tasks, Go, Python, and task by task against each harness. The page keeps to results and tables; OpenCode and Kilo read 26.5% (30 tasks), not 26.6%.
