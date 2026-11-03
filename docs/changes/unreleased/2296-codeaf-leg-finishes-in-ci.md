---
kind: internal
title: The cmd/codeaf touched leg gets a 25-minute timeout
pr: 2296
surface: [build]
invalidates: []
---

The package's serial suite measured 17.8 minutes on the CI runner, over the
15-minute timeout, so tests after the cut went unchecked. Only the codeaf leg
changes; the other legs keep 15 minutes.
