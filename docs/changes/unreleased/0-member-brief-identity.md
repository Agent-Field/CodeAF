---
kind: fixed
title: A member's brief follows its own start line, not its handle
pr: 0
surface: [engine, app]
invalidates:
  - "members.brief read every start line addressed to the member's current handle, so after a rename or a handle reused by a later member it could return someone else's brief and lose the original. A membership now keeps the id of the exact start line that made it (`start_entry`, written by `crewStart` in `cmd/codeaf/appcrews.go` for the desktop app and by the terminal's start), and the brief is that one line, read by id from the log."
  - "A member started before `start_entry` existed has a brief only when the log proves it: exactly one manager start addressed to its recorded key, or the only start ever addressed to its handle in a log that still begins at its first entry with no former member of that name. Otherwise the brief is absent; nothing is parsed from words, taken from its first message, or credited to the current manager."
  - "members.brief no longer reads the whole traffic log of the crew (`ReadTraffic(...,0)`); it reads one entry by id."
  - "A scan of the log for an older membership holds one candidate and a count of at most two, reads no more than the file's size when it opened, and fails with an error rather than reading as empty when a line is past 1 MiB or a file is far past rotation size."
---

The id lives on the member record in `teams.json` beside the other member
fields and survives builds that do not know it.
