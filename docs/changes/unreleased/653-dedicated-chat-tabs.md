---
kind: changed
title: A dedicated tab strip over the chat, with the task trail on its own row beneath
pr: 653
surface: [chat]
invalidates:
  - "The frame's first row was ONE pinned row that both bars shared: a conversation drew a single dim crumb (`main`, with `▾` after it where the switcher had somewhere to go) and a task page drew its trail, its state and its ✕ on that same line. The frame now places the tab strip above the task breadcrumb and separate metadata row, while a conversation draws no redundant task header. Anything reading the room header, the breadcrumbs or the ✕ at frame row 0 is stale: [app.roomHeadRow] is the row number now, and it is the tab strip's height."
  - "The conversation's own name and the picker were reached by pressing that single crumb (`crumbSwitch`). That crumb kind is gone. The name is the lit tab and the picker is the control at the strip's right end."
  - "Over a shared engine handle (`Options.SharedAgent`) the keeper dropped the whole aside on the way out, so a switch left the unsent sentence to whatever the draft debounce had last written. [app.stow] now writes the composer down under the conversation's own identity before it returns, in both worlds."
---

The first row of the chat frame is the conversations this window has been in,
drawn as tabs in the order it FIRST entered them — not in switch order, so a tab
does not move out from under a finger. Each tab has its own background and padded
label and close targets; the selected tab has the stronger surface, while terminals
without background colour retain brackets. The `+` control opens New chat, and `×`
or `ctrl+w` closes a tab or asks what to do first when that conversation is working.

Clicking a tab switches through the keeper's own doors — `bringForward` for a
conversation this process holds, `hopStart`'s two refusals for one it does not —
so the mouse journey and `ctrl+k`'s journey are one implementation. Clicking the
tab already in front comes back out of a task page and does nothing on the
conversation itself. `Chats` at the right opens the same card `ctrl+k` opens and
adds the number of tabs outside the viewport when space permits. Roomy strips
scroll horizontally instead of shrinking names; narrow ones keep the selected tab
and the `Chats` fallback.

**A tab claims navigation, not a running session.** Every shipped door holds one
connection per conversation, so switching tabs or going Home stops nothing. A
working tab asks whether to keep running, stop work or cancel before it is dismissed;
an idle tab closes immediately.

The task breadcrumb sits beneath the strip, followed by a separate metadata
row with the available Stop action. Kin rows remain part of the task header.
A conversation has no redundant task-header row; exact vertical spacing adapts
to the frame size.
The strip stands down on the two floors the conversation's bar stood down on —
under `roomHeadFloor` columns, or with no breathing row above the draft.

Nothing on the strip reads the disk or the wire on a frame: the tabs come from the
keeper's map, the previous-stack and the surface's own title, the front
conversation's canonical key is memoised against the file it was taken from, and
the drawn line is memoised on width, ink, hover, picker availability and complete
tab identities. The window remembers at most 32 tabs; past that, the one nobody has
been in for longest falls off while the selected tab remains.

Switches retain each conversation's draft store and restore only the incoming
owner's slots. The outgoing plain-text export cannot become a new chat's draft.
Round-trip coverage pins both chats' text and caret positions.
