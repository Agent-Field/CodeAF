---
kind: added
title: opt-in team sharing between paired computers
pr: 2080
surface: [chat]
invalidates:
  - >-
    Teams stayed in one profile. Sharing is now available as a local opt-in:
    Settings → General → share teams is off by default on each computer.
    Nothing changes until both paired computers turn it on; unpaired computers
    and computers with sharing off keep their teams to themselves.
  - >-
    Traffic existed only on the writing computer. Once both computers share
    teams, bounded recent history appears in a separate read-only mirror;
    local logs and delivery remain local. Turning sharing off stops carriers,
    mirror writes and ownership lookups immediately, keeping received teams.
  - >-
    Team member paths and delivery cursors named one computer's files. With
    sharing on, portable member identities and device-scoped cursors allow
    continuation through Home's existing question and safe local delivery.
    Positive remote ownership refuses unreachable sends before writing;
    mixed sends name recipients not reached. Sharing off retains prior behavior.
  - >-
    Shared vault helpers applied team-specific merge and removal safeguards
    to every scope. Existing carriers now retain their original tie, tombstone
    and apply behavior; only team carriers opt into the new safeguards.

---

Unpaired computers retain their file order and original local file formats;
sharing-off computers retain the same behavior. With sharing on, background
refresh reads without repair writes and pairing with replacement moves local teams aside and keeps Traffic logs. Mirrors use the locally known paired
computer list when available. Unchanged rounds perform no session-world discovery.

Sharing failures now log once and retry without stopping other profile state,
chat continuation or local delivery. Mirror filtering adds no directory requests
to vault rounds. Rotation preserves the recent published window, display refresh
does not skip local actions, and each hosted window sees mirror changes. Computer
names fall back to "the other computer"; unmatched history links name that
computer's history. Packet decisions and escalations refuse unreachable targets
before mutation.
