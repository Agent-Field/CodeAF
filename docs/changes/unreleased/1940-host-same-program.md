---
kind: fixed
title: Remote chat starts the same installed program
pr: 1940
surface: [chat, engine, remote, docs]
invalidates:
  - "Remote chat always started codeaf on the far machine, even when the local program had another name. It now selects the matching executable and explains a fallback to codeaf only when that name is absent."
  - "A newer incompatible engine refused an older bridge even when its executable was available. On supported systems the bridge now hands over once, preserving its arguments and connection."
  - "Version-mismatch guidance recommended stopping an engine while its windows could restart it. It now gives update guidance or asks for work to finish and all workspace windows to close before reopening."
---

Named installations keep their program name in update notices. All names still
share one engine per workspace. Differing wire versions stop cleanly with update
guidance; neither machine is updated automatically. Executable handoff is supported
on Unix systems; Windows retains the explicit update refusal.
