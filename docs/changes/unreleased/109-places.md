---
kind: added
title: a conversation can be about places beyond the one it opened in, and work there lands as a diff
pr: 109
surface: [chat, engine, build]
invalidates:
  - "furrow was optional, absent-not-broken, installed by the person; it is now embedded in bin/aforge (go:embed, extracted version-stamped under the state root), every build carries it, and the SIZE-BUDGET ratchet was raised for it by owner ruling 2026-08-31."
  - "a conversation had exactly one directory, immutable after launch, and no picker existed anywhere; there is now a referred-places set on the conversation (feeding the SAID rung of the task ground ladder, never a second resolver) and a /folder picker that is also the forming card's g correction."
  - "/attach refused a directory with 'is a folder · attach a file'; a directory is now the door to referring a place."
---

The wave for issue #108: the window is where you sat down; the conversation is about
places. Referred places accrue from evidence (a named path, @ on a directory, a folder
drop, a kept ground resolution) and are shown as consequences — a ground row, a
changes-waiting chip — never as an inventory. Writes aimed at a referred place go
through a standing tree and land through a diff the person reviews; the standing place
stays direct. Furrow is embedded so the tree is byte-exact everywhere, dirty trees and
plain folders included.
