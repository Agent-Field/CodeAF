---
kind: changed
title: the reflex tier ships on mistralai/mistral-nemo instead of nex-agi/nex-n2-mini
pr: 140
surface: [chat, engine]
invalidates:
  - >-
    The shipped reflex default was nex-agi/nex-n2-mini, in DefaultReflexModel
    and in all three crew presets. It is now mistralai/mistral-nemo everywhere
    a default names the tier; nex-n2-mini appears only in historical comments
    about what it did there.
---

nex-n2-mini's endpoint accepts the reasoning disable and thinks anyway, so every
reflex call ran at the 2,000-token recovery ceiling and one overlong thinking
pass tripped the session-wide "answers nothing at its budget" fallback to the
low tier. A reflex model that must think defeats the tier's economy; mistral-nemo
honors the disable, so the 200-token ceiling is honest again. The failure-path
redesign itself (per-endpoint quirk, provider rotation before the model falls,
a breaker instead of a session-permanent abandon) is #138.
