---
kind: fixed
title: an unstamped binary says it was built outside a git checkout, not without make build
pr: 1151
surface: [build, docs]
invalidates:
  - "`codeaf --version` on a build with no revision said it was \"built without `make build`\", and the design note for that row recorded that the tree embeds no `vcs.revision` under a plain `go build`. Both are false on go1.26.5: a plain `go build` inside the checkout embeds `vcs=git`, `vcs.revision`, `vcs.time` and `vcs.modified`, internal/buildinfo falls back to them, and `--version` prints a full pseudo-version such as `v0.2.2-0.20260918035413-2ab365d6cb3e` with no linker stamp involved. The revision goes missing when there is no VCS to read — a tree that is not a checkout, an archive, a vendored copy, `-buildvcs=false` — and `make build` does not rescue that case either, because its own stamp comes from `git rev-parse --short HEAD`."
---

The sentence now names the condition rather than a target, because the target cannot fix the case
where the sentence appears: both roads to a revision need a checkout. The assertion in
`polishrows_test.go` required the old wording, so it changed too, and its comment carries the
measurement that settles it.
