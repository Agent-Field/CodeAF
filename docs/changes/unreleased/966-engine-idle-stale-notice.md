---
kind: fixed
title: an idle conversation on an older engine build is named on the way in
pr: 966
surface: [chat, engine]
invalidates:
  - "The stale-host entry notice (cmd/aforge's clearStaleEngineHostFor) used to fire only when the older same-wire host reported Busy (work in flight). A host holding only an IDLE conversation — the turn finished, the person stepped away — was retired silently, and the window opened on the fresh build without a word. It no longer does: the notice is owed whenever the older same-wire build is the one answering, and it names what was held — still holding work, only holding the conversation, or already gone and current now."
---

A session host outlives the build that started it by design, and the busy-host
notice from #691 covered the case where the older build was still holding work.
The owner's ordinary case is the other one: rebuild, leave the conversation
idle, come back — the older host reads as not busy, so it was asked to go, went,
and nobody was told the engine answering had been a build behind. The notice now
speaks for that case too, so a person stops debugging a fix that had simply not
reached the conversation yet.
