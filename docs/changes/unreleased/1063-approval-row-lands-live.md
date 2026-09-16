---
kind: fixed
title: "\"ask before running\" reaches the gate you are already behind, and /status names it"
pr: 1063
surface: [chat]
invalidates:
  - "The `ask before running` row in `/settings` (`tools.approvalMode`) used to land on the NEXT session only — its own hint said so — because `internal/tui3`'s `applySetting` had live-push hooks for the work, icons, lane, lane-guard and routing rows and none for the three safety rows. It pushes now, through the same `ApplyApprovals` seam `/permissions` drops a banked rule through (`cmd/codeaf`'s `applyV3Approvals`), and so do `tools.approval` and `tools.bashPatterns`. The hint reads: \"A change reaches this conversation straight away, unless the panel says it lands on the next session.\" Where the rules cannot be rebuilt — and on a plain launch, where this machine's engine holds the conversation and there is no take-back door to its gate — the row is still saved and the panel's foot line says `saved · from the next session`, which is /permissions' own sentence."
  - "The YOLO badge and the gate did NOT always agree, in both directions. `app.approvalPosture` reads the profile row live (at boot and at every turn end), so setting the row to `allow` in the panel lit the badge over a gate that went on asking, and setting it back to `prompt` put the badge out over a gate that was still wide open — the false-safety claim #322 and #325 were about, arrived at from the other side. The badge and the gate now move on the same keystroke, and on the failure branch the badge stays with the gate in force rather than following the file."
  - "`/status` did NOT list everything the status line knows: the gate had no line on it at all, because the only spelling of that fact was the `YOLO` badge, which is drawn solely over an open gate. `/status`, `/status --json` and the phone's status sheet now carry `approvals` with the posture in words — `prompt`, `allow` or `deny` — read from the same single posture the badge reads. It sits after `search` and before the telemetry words, and the segment is no longer written into the page a second time. A remote session whose engine carried no posture still has no line. The note's label column is measured from the widest label it carries, so `approvals` widens it by two cells — three crew tests that asserted `\"\\ncrew\"` plus exactly five spaces were asserting about the other rows and now read the line by its label."
---

The panel's own law is that a row a person watched themselves change must not
then do nothing, and the one row where breaking it is a safety claim rather than
a slow answer was the row that broke it.
