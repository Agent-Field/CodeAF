---
kind: changed
title: Ctrl+t starts a new chat and the task roster moved to alt+t
pr: 653
surface: [chat]
invalidates:
  - "Ctrl+t handed the keyboard to the task roster in a conversation. It now opens the new-chat start page — the same door the `+` at the end of the tab strip presses — from the message box, from a room, and from a roster that is holding the keyboard, which gives the keyboard back on the way. The roster's hand-off is `alt+t` (`⌥t`), the same letter under the modifier its `alt+w` widen chord already uses."
  - "Nothing else that spends ctrl+t changed. The model picker's ctrl+t still walks the reasoning effort of the row under the cursor, and home's ctrl+t still starts a conversation in the folder of the row under the cursor; both are read above the new-chat rung and take the key first."
---

The strip is drawn as tabs, so the key every browser opens a tab with is the key
people press at it, and it had no chord at all. It opens the page rather than a
conversation: nothing is created until the first message is sent, `esc` comes
back, and the conversation left behind keeps its draft, its attachments, its
reading position and its running work. Pressing the chord again on the page keeps
what has been typed there rather than building a second one.

On macOS `⌥t` composes to `†` where Option is not set as meta, which is the same
condition the surface's existing alt chords carry; it is in the table that draws
the "turn on use option as meta" note, and *getting started* has the setting.
