---
kind: changed
title: DeepSWE results identify the tested harness versions and release timing
pr: 1877
surface: [docs]
invalidates:
  - "The DeepSWE comparison listed scores without the tested harness versions, leaving its OpenCode generation unclear. The results page now identifies the nine competing versions, including OpenCode 1.18.30, and senior-dev's public integration in #1488 and first stable release v0.5.0; versions.csv records versions, revisions and public release references."
  - "The setup dated all competing runs to September 11. Saved task metadata spans September 10–11 UTC; senior-dev ran on September 12 UTC."
  - "The benchmark index listed only CLI footprint measurements. It now also links the DeepSWE task-quality comparison and its version evidence."
---

The comparison names the measured versions and the patch releases available at
launch without extending the results to newer versions. Published scores are
unchanged.
