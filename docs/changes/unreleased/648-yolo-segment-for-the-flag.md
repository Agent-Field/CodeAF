---
kind: fixed
title: the status line names the gate --yolo opened
pr: 648
surface: [chat]
invalidates:
  - "The `--yolo` flag does not raise the YOLO segment, so a run started with it has the gate open and the segment empty — that was true, and #326's own entry and the manual's status-line table both said so. The flag now hands its forced posture to the surface, and `aforge chat --yolo` and `aforge resume --yolo` draw `YOLO` on the status line for the whole session, with nothing written into the profile."
  - "The YOLO segment is the `tools.approvalMode` row in the profile, read live, and nothing else on a local session. It is now either that row or a posture the LAUNCH forced: `tui3.Options.ApprovalMode` was the engine's row over --host and empty locally, and it is now the one field any door hands a posture down on. A launch that forces nothing still hands nothing down, so the profile is still re-read at boot and at every turn end."
  - "internal/tui3's `app.hostApproval` holds the engine's carried posture. That field is now `app.handedApproval`, because a local `--yolo` launch fills it too; `approvalPosture` prefers whatever was handed down and falls back to the live profile read."
  - "A repository that sets `tools.approvalMode: allow` in its project settings raises the segment. It still does not — the gate resolves that row through the workspace layer while the segment reads the profile — and that gap is unchanged here. It is `docs/conversations-design.md` section A7, not #325."
---

A safety claim, so it is a number of its own rather than a paragraph inside #322.
An absent segment is the surface saying every tool call will be asked about, and
under the flag it said that over a gate that was open for the whole run.
