---
kind: changed
title: a quiet conversation left alone is let go of, so a window's memory can come back
pr: 151
surface: [chat]
invalidates:
  - "Nothing evicted a conversation. `internal/tui3/keeper.go` said a window's memory grew with every conversation opened and only `/quit` or `ctrl+w` gave one back. Past twelve open, the keeper now lets go of the coldest quiet one — idle fifteen minutes, nothing turning, nothing waiting, no unseen news, no words still in its box."
  - "The manual said there is no cap and nothing closes a conversation for you. Doors still never refuse another conversation, but past twelve open a quiet one left for fifteen minutes is let go of, with `let go · <name> — quiet a while — open it again from home` (or `its work keeps running` when the engine keeps it)."
  - "`docs/changes/unreleased/137-remove-conv-cap.md` said nothing evicts. That was true of the eight-cap removal; it is not true now. The eight-cap refusal is still gone."
---

The owner's ruling that no door may refuse a conversation for how many are already
open still stands. What was missing was anything that gave the cost back. The keeper
sweeps instead of refusing: a soft ceiling of twelve (the switcher card's own row
count), LRU by how long ago you left, and only a conversation nothing would be lost
from. A window whose held conversations are all busy sails past the number.
