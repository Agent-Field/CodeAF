---
kind: added
title: teams and crew routes follow paired computers
pr: 0000
surface: [chat]
invalidates:
  - >-
    Teams stayed in one profile. Team fields, portable memberships and manager
    choices now travel encrypted between paired computers within the ordinary
    round delay; deletion beats a stale copy and repair is deterministic.
  - >-
    Traffic existed only on the writing computer. Other computers now display
    a separate atomic history mirror; local delivery logs still contain only
    local lines. Each writer shares up to 32 lines and 16 KiB per team, 64 KiB
    across teams, at most once every 30 seconds, shortening text past 2 KiB.
  - >-
    A send could claim success for a member held elsewhere. Sends with no local
    recipients now refuse before writing; mixed sends reach local recipients
    once and name those not reached. Only positive remote ownership refuses;
    failed discovery and missing paths preserve local delivery. Member-opening
    gestures use Home's visible continuation question, while hosted surfaces
    say the chat is on the other computer.
  - >-
    Continued chats reused delivery positions from another computer. Paired
    chats now save their device beside those positions and discard foreign ones,
    including untagged positions beyond the receiving computer's local log.
  - >-
    Team bookkeeping could fail a completed continuation. It now reports a
    notice without releasing the lease, and later synchronization heals paths.
  - >-
    Crew seat models travelled but pinned routes and disabled providers stayed
    local. Those choices now follow the seat pins as preferences.
---

Unpaired computers retain their file order and original local file formats;
background refresh reads without repair writes. Pairing with replacement moves
local teams aside and keeps Traffic logs. Mirrors use the locally known paired
computer list when available. Unchanged rounds perform no session-world discovery.

Sharing failures now log once and retry without stopping other profile state,
chat continuation or local delivery. Mirror filtering adds no directory requests
to vault rounds. Rotation preserves the recent published window, display refresh
does not skip local actions, and each hosted window sees mirror changes. Computer
names fall back to "the other computer"; unmatched history links name that
computer's history. Packet decisions and escalations refuse unreachable targets
before mutation.
