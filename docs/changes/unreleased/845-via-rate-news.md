---
kind: fixed
title: the provider and the live tok/s no longer vanish at random
pr: 845
surface: [chat, remote]
invalidates:
  - "`via <provider>` and tok/s were thought to drop out at random, maybe on a follow-up. There were six causes, each with its own trigger, and each is fixed."
  - "A reasoning level set in /model or with --reasoning took the live tok/s off the status row, because the row lent the model field `id:level` while it drew. The field is no longer lent, so the rate shows with a level set."
  - "The seam's `via` still hid when the vendor served its own model (`deepseek/…` by DeepSeek, `z-ai/…` by z-ai), even though the owner had ruled against hiding it. It now shows whoever served, on the seam and in a task room. Only the /status `served` row keeps the rule."
  - "A rescue that failed blanked `via` until the next answer, and a turn interrupted mid-rescue kept saying `slow · trying X…` for up to ten minutes. The rescue now has a slot beside the last sighting, so a failed one hands the row back to the machine that answered last. A rescue is only promised while the turn runs."
  - "A title or memory errand finishing on another lane overwrote the answer's sighting and blanked `via`. Hidden roles no longer reach the status line's desk."
  - "`via` appeared only once a whole step had finished, so the first answer, or a follow-up after ten quiet minutes, showed none while it was being written. It now names the machine the live phase reports."
  - "The conversation's rate and machine were filed under its model id, which moves between the engine and the window (a mid-turn pick, a fallback hop, a change from another window). They are now filed under the conversation first. The `phase` and `lane` wire frames carry `session`, and an older host without it still reads by model."
  - "An engine host from before the news frames sent no provider or rate, and nothing said so. The welcome now carries `news`. A window whose engine sends neither the flag nor any news frame says once, after an answer, that its engine is an older aforge."
---

The wire version does not change. A status line must not refuse a conversation, so the older-engine case is reported with a note and never refused.
