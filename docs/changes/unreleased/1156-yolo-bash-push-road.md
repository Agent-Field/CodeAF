---
kind: internal
title: a diagnosis note pins the road a chat door's bash push took under --yolo
pr: 1156
surface: [chat, engine]
invalidates:
  - "Nothing about behaviour changed — this entry exists because the check job
    demands an entry for every pull request, and this one carries a diagnosis
    note and one deliberately failing test instead of a fix. What the note
    establishes: a chat door's own bash `git push` under `--yolo` is allowed by
    the approval gate (the flag replaces the policy default with allow and
    `git push` is on neither floor table) and never reaches the git guard,
    which by design leaves a Person-headed session unguarded; and task landing
    protects a checkout by branch NAME, never by asking whether the run
    created the branch it is standing on."
---

`docs/notes/chat-door-pushed-a-task-branch-under-yolo.md` answers the three
questions — which road the push took, how the answer differs between a
person's session and `--yolo`, and what the landing asks about the branch it
merges onto — with file:line for every claim. The failing test
(`TestAYoloRunDoesNotPushToABranchItDidNotCreate`, `internal/session`) replays
the three bash lines the incident actually ran down the real pre-action chain
and records the answer the gate gives today: `default → allow`. No fix rides
with it; the note's last section says what the obvious fix would be.
