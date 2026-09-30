---
kind: fixed
title: benchmark cells cannot see upstream fix branches
pr: 1621
invalidates:
  - "Benchmark cells cloned every upstream branch before running. They now fetch only the requested base commit or remote HEAD."
---
