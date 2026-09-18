---
kind: fixed
title: "review conclusions require a command that bears on the claim"
pr: 1206
surface: [chat]
invalidates:
  - "A review backfill treated a recorded shell chain as one command, so real test, build, and vet segments were lost. Backfill now judges each segment and retains only exit-bearing runners. Holds requires every declared check to run, does not hold requires none, and check records a leaf note."
---

The review round keeps declared Checks: as its first choice. When a worker did
not declare one, it removes one leading `cd` wrapper, splits recorded commands
on `&&` and `;`, applies the shipped runnable, invocable, and audit law to each
segment, and retains only test, build, and vet style runners. The committed c249
fixture produces 11 checks and 495 characters, rather than 37 commands and
26,312 characters: nine `go test` segments, one `go build`, and one `go vet`.
Reads, writes, status commands, and `plandb done` are excluded.

A `holds:` conclusion requires every declared check in the trajectory. A
`does not hold:` conclusion requires no command evidence, so a reading checker
can report a defect on an empty or partial contract. A `check:` answer is stored
as a note on the checked leaf.

The check seat remains read-only. This does not change `verifyOnlyBash` or
`refuseOutsideDoor`.
