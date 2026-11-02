---
kind: fixed
title: A failed command step says its exit when it asks
pr: 2209
surface: [engine]
invalidates: []
---

A command step that fails now asks with the same fact its header shows:
`mustfail is not green: exit 1 · try again, send it back, or stop the job?`, and
its try reads `not green · exit 1`. Before, both said `no result`.
