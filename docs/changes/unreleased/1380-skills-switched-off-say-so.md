---
kind: fixed
title: skills that are switched off say so instead of looking like a feature that was never built
pr: 1380
surface: [chat, engine]
invalidates:
  - "The skills wave is not unconditional: skills are read through memory, so a profile with memory.enabled off has no shelf and no use_skill. It used to render as silence and the chat would answer that codeaf has no such mechanism. It now renders as one line saying they are switched off and naming the setting."
---
A person with eighty-one SKILL.md folders on disk asked the chat whether it could
use skills and was told codeaf has no such mechanism. The binary carried the
wave. `memory.enabled` was off.

Every part behaved as designed. The launch import returns when memory is nil, the
catalog renders zero bytes for an empty shelf, and `use_skill` is withheld rather
than present and refusing. The model was then left with no shelf, no verb and no
sentence about either, so it reasoned from the silence and denied a feature that
had shipped, correctly from the inside and wrongly about the world.

The absent-not-broken law stops a model planning a reply around a call that can
only refuse. It does not stop a model reasoning from the absence. So it needs a
companion clause rather than an exception: when a capability is withheld by a
setting, something has to say so, or "absent because impossible here" and "absent
because it does not exist" are the same silence.

The catalog now renders one line when the machine has skills and this session
cannot reach them, naming `memory.enabled` and saying the skills exist. A machine
with no skills still pays nothing, which is what the emptiness law is actually
about. The `/skill` picker reads the folders rather than the shelf, so it listed
skills that could not be attached: each row now carries the reason beside the
folder's own warning rather than instead of it.
