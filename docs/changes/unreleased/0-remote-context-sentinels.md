---
kind: fixed
title: Preserve context deadline and cancellation errors for all remote callers
pr: 0
surface: [remote]
invalidates:
  - "Resident deadline and cancellation replies previously lost their context error identity. All remote callers now preserve the sentinels when those replies arrive before the caller stops waiting."
---

All remote callers receive errors matching `context.DeadlineExceeded` or
`context.Canceled` for the corresponding resident context errors, regardless of
which side reports cancellation first. This includes task root Review and Apply
and process history reads. Error text, typed work refusals, saving wrappers and
the warning for unconfirmed task root outcomes are unchanged.

A resident error whose whole text is exactly "context deadline exceeded" or
"context canceled" is treated as that context error. Longer messages and text
with surrounding whitespace are not matched.
