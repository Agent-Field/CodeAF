---
kind: fixed
title: Browse chat names before switching and return to the same task reading position
pr: 653
surface: [chat]
invalidates:
  - "Ctrl+k switched immediately and dismissed its list after a short pause when quick switch was enabled. It now always previews choices until Enter, a numbered shortcut, or a row click. The quick switch setting applies only to the distinct ctrl+tab chord."
  - "The conversation switcher was keyboard-only, could hide its selected row on short frames, and abbreviated long names without a reading preview. Its visible rows and fold now accept clicks, its selection stays visible, and long selected names get reading space below the list. The modal consumes backdrop mouse input."
  - "Reopening a task always reset its folds and followed the latest output. This window now retains bounded, owner-scoped reading positions and display choices across task navigation, including successful hosted reads."
---

Ctrl+k keeps the underlying conversation still while the person reads the list.
Enter or clicking a row opens it; Escape cancels. Ordinary terminal input cannot
reliably distinguish modifier release, so a pause is not treated as a choice.
No user settings are rewritten. Ctrl+tab retains the configured immediate-switch
behavior where the terminal delivers that separate chord.

Task reading state is in-memory and bounded to 64 recently visited task views,
with up to 240 block settings per view. It retains no transcript or subscription.
If the old scroll anchor has fallen out of the journal tail, reopening starts at
the oldest retained row. Adaptive-run graph pages and process restarts are not
covered by this reading cache.
