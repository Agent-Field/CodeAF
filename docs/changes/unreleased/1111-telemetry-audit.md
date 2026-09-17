---
kind: fixed
title: Usage counts from a hosted chat carry real numbers, and nothing without a version ever leaves
pr: 1111
surface: [chat, docs]
invalidates:
  - "A chat attached to the session host reported 0 turns, 0 model calls and 0 tool calls in its session_ended usage event; it now counts them from the events it receives."
---

Three fixes from the first day of real usage counts. A chat that attaches to
the workspace's session host now counts its turns, model calls and tool calls
from the events the host sends it, so `session_ended` carries real bands
instead of zeros (a `--no-host` chat already counted in-process and is
unchanged). An event whose `codeaf_version` is missing or `unknown` is dropped
on the send path itself and never leaves the machine, whatever switched the
opt-out ladder on. And the smoke tests, which build and run a stamped binary,
now run it with telemetry off, so a test run no longer counts as a new install.
