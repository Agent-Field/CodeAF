---
kind: fixed
title: Retire expired holds on work confirmation and keep late saves as separate copies
pr: 2381
surface: [chat, engine]
invalidates:
  - "`ConfirmWork` previously returned an expired-hold refusal without retiring the local batcher, allowing a late save after the check to publish onto the original chat."
---

When `ConfirmWork` finds that this computer's chat hold has expired, it now
retires the local hold the same way `CheckLease` does. A late save after this
computer has noticed its hold expired stays local and is kept as a separate copy
when it syncs, without replacing the shared chat's saved turn, and a fresh
continuation is needed for new work.

When an un-displaced batcher recovers from an unreachable sync service, it
renews its hold with a heartbeat before publishing queued turns so offline saves
land on the original chat when no other computer took it, or branch when another
computer took it.

The relay's own publish rule is unchanged: a publish still renews an expired
hold that nobody took, so builds that do not heartbeat first keep their outage
recovery. Refusing that publish at the relay needs a versioned protocol change,
tracked in #2631.
