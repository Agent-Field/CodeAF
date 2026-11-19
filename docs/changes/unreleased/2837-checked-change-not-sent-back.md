---
kind: fixed
title: A short finished reply after the turn checked its own change is no longer sent back
pr: 0
surface: [chat, engine]
invalidates:
  - "Since #2834, a tool-free reply of one or two plain paragraphs that came straight after codeaf's `[silent]` note or `[held]` demand was always sent back with `it wrote down where it was and stopped · asking it to go on`. It is now left alone when this reply had already checked what it changed: a step that may change the work succeeded, a look at the work was then asked a question (`view_image`), and no step failed or was held after that look. A `read` of an image describes and is not a check. Stalls that changed nothing, never looked, or failed after their look are still sent back. The cap and the shared budget are unchanged."
---

On the connected-canvas qloop, flyer t2 ("date moved to sat 14 march, fix the
flyer") was sent back in both run 14 and run 15 after it had patched the date,
asked a look whether the date line read right, and written a finished
two-paragraph reply; both times the model answered `[no change]`. Across the
baseline to run 16, five short replies came right after a request for a note:
the two flyer replies had checked a change, and none of the three stalls had.
