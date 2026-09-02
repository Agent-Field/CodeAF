---
kind: fixed
title: dev is green again — the ground test shows its chip on a path that exists
pr: 305
surface: [build]
invalidates:
  - "internal/exec TestNothingInTheTreeNamesARemovedWorker was red on dev from #303 onward because internal/tui3/ground_test.go used `internal/swepro/` as sample text — no longer true; the sample is `internal/manual/`, the same width, and the guard is unchanged."
---

The guard that nothing in the tree still names the worker removed in #227 was
doing its job: a rendering test had picked the removed directory as the text to
draw a chip around. Any path of the same width shows the chip equally well, so
the sample changed and the guard did not.
