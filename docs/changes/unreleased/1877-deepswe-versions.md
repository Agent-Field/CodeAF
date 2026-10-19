---
kind: changed
title: DeepSWE results identify the tested harness versions and release timing
pr: 1877
surface: [docs]
invalidates:
  - "The DeepSWE comparison listed scores without the tested harness versions, leaving its OpenCode generation unclear. The results page now identifies all ten versions, including OpenCode 1.18.30, with public release dates and links; versions.csv records source revisions, artifact checksums and saved attempt time bounds."
  - "The setup dated all competing runs to September 11. Saved task metadata spans September 10–11 UTC; senior-dev ran on September 12 UTC."
  - "The benchmark index listed only CLI footprint measurements. It now also links the DeepSWE task-quality comparison and its version evidence."
---

The comparison names the measured versions and the patch releases available at
launch without extending the results to newer versions. Published scores are
unchanged.
