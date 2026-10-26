---
kind: added
title: Private task copies behind an experiment switch, and removed computers keep working locally
pr: 1944
surface: [chat, engine, remote, docs]
invalidates:
  - "A delegated task could only work in, and land into, the project the chat opened. With CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP=1 and an absolute CODEAF_HOME on Linux or macOS, a new chat's tasks work in private copies, may span several admitted folders, and come home only through /task review and /task apply. Without the switch, and always on Windows, tasks land exactly as before."
  - "/task had no review or apply step. /task review <number> <root> and /task apply <number> <root> now exist; in a chat without private task copies they say so in one sentence and change nothing."
  - "The Raft relay refused a publish whose signed body carried frame_hints. It now carries them as bounded, optional metadata; no shipped client sends them yet."
  - "Since #1967 a window that learned its computer had been removed refused every send, even in a new chat. A removed computer now keeps working on chats it held or never shared; only a chat another computer took stays read-only there."
  - "A move that failed because the other machines could not be reached said only other machines unreachable. It now says the chat could not be continued here and that the words are still in the box."
  - "A native chat could appear twice in activity and label finished work incomplete because its internal state folder was mistaken for its identity. Activity now joins the live and saved rows under the chat's own identity."
  - "Transferred task history with a fixed worker journal name had no transcript citation. Completed runs now reopen that verified local journal even without a plan database."
---

Private task copies, multi-root workspaces and explicit Review/Apply exist behind
`CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP=1` with an absolute `CODEAF_HOME` on Linux and
macOS. Without the switch, a new chat persists no workspace policy, its tasks land
as before, and its task checkpoint and carrier keep the formats older builds read.
A saved private-task policy travels with its chat and needs no switch on a
compatible receiving computer. Windows has no private-task Apply, so it never
enables ownership, even for a saved policy.

The experiment's limits:
- every computer a private-task chat moves to needs a build that understands its
  checkpoint and carrier, because an older build can take it and drop its tasks;
- seal cost grows with retained private roots, because complete histories are
  captured and screened again;
- workspace sets keep their private copies, so there is no automatic storage
  reclamation yet;
- for Git projects, the profile and the destination's Git index must be on the
  same disk; Apply refuses before writing anything otherwise.

Apply uses the exact reviewed snapshot, preserves conflicts and unrelated staged or
dirty work, and keeps the private copy.

For every chat, task cards recover their worker transcript and recorded model after
a restart or a move. Typed folder targets survive asynchronous listings, and a
failed move keeps the typed words and never sends them.

Native chat activity uses the chat's identity rather than its internal state
folder, so one completed task appears once and says done even while an older
index reading still says running. This also repairs the duplicate activity rows
on dev. Transferred worker journals with either a timestamped name or the fixed
worker name reopen locally without a plan database. Finished tasks remain in
Tasks; the active sidebar deliberately leaves them out.

Warm-transfer frame hints ride the Raft relay and stay off by default:
- an invalid set is dropped rather than refusing the publish;
- Home listings omit them;
- every cluster member must understand them before a device sends them.
