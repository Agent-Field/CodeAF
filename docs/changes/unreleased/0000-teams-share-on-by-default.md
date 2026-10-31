---
kind: changed
title: team sharing on by default for paired computers
pr: 0000
surface: [chat]
invalidates:
  - >-
    Share teams was off by default and both computers had to turn it on. It is
    now on by default for paired computers, and turning it off on either
    computer stops sharing. Unpaired computers are unchanged.

  - >-
    The raw sharing preference selected refresh, cursor and history behavior.
    Now only preference on plus pairing enables those paths; unpaired computers
    retain their original clocks, completion, repairing reads and cursor bytes.
  - >-
    A damaged configuration could enable team sharing. Now missing or empty
    choices read on, while unreadable files and invalid values read off.
  - >-
    A first merge could leave duplicate team names and discard appointments
    without a copy. Now teams.before-sharing.json preserves the original once,
    duplicate names stay unchanged, the older global manager stays global,
    the other manages an ordinary team. Cleared appointments fill only an empty
    or matching former-manager slot; existing former appointments stay.
  - >-
    Replacement could set teams aside without mentioning them or the saved path.
    Now chat and interactive terminal confirmations include teams even without local chats,
    non-terminal --replace proceeds as before, and the receipt names the preserved team file.
  - >-
    Opening a local team member could wait on or be refused by ownership discovery.
    Now it takes the same local path as Home; positive remote ownership still refuses.
  - >-
    Traffic windows could publish every 30 seconds. Now they publish at most every
    2 minutes while team record edits remain undelayed.
  - >-
    Turning sharing off left retained mirror history in session readers and cached
    screens. Now off or unpaired hides that history and rejects late mirror rows,
    preserving the file and retiring cached provider digests; hosted windows follow
    their engine's sharing. Previously exchanged teams and encrypted vault copies stay.
  - >-
    The manual did not explain the spending scope of a shared cap. It now states
    that each computer meters spending separately, including continued members.

---

Teams, their members and settings, and recent Traffic as read-only history now
share by default between paired computers. Settings → General → share teams
stays local to each computer. An explicit off stays off, stops sharing
on that computer and keeps teams already received, including encrypted vault copies. Unpaired computers are unchanged.
