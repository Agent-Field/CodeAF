---
kind: added
title: opt-in team sharing between paired computers
pr: 2080
surface: [chat]
invalidates:
  - >-
    Teams stayed on the computer they were made on, even between paired
    computers. They can now be shared, but only by choice: Settings → General →
    share teams is off by default on each computer, and teams travel only when
    both paired computers turn it on. With it off, or unpaired, nothing changes.
  - >-
    Traffic existed only on the computer that wrote it. Once both computers
    share teams, the other computer's recent Traffic shows as read-only history
    in a separate mirror; delivery still reads only this computer's own log.
  - >-
    A team member whose chat was on another computer could only be opened
    there. With sharing on, opening it asks Home's `Continue this chat here?`,
    and a send that could not arrive is refused before anything is written.

---

Turning share teams off stops all team sharing at once and keeps teams already
received. Existing vault carriers (credentials, provider keys, preferences,
memories) keep their merge, removal and apply rules exactly; only the team
carriers use the new guarded rules. The prompt and tool descriptions are
unchanged.
