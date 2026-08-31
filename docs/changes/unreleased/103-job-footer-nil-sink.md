---
kind: internal
title: a job row with no sink no longer panics the footer
pr: 103
surface: [engine]
---

The carry-on tests planted job rows without a sink and the job footer's walk
took that as a recovered nil-pointer panic, about eighteen per run, each one a
tool result silently losing its footer. The read is nil-safe now and the
fixture carries a real sink.
