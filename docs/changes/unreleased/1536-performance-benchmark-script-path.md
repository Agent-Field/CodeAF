---
kind: fixed
title: Performance benchmark examples use the script's real path
pr: 1536
surface: [docs]
invalidates:
  - "The benchmark examples pointed to docs/benchmarks/measure-cli.sh, which does not exist. The script lives under docs/benchmarks/performance/measure-cli.sh."
---

The performance benchmark README and the script's own usage example now resolve
the executable from the repository root.
