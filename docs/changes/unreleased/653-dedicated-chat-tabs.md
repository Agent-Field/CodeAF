---
kind: changed
title: A dedicated tab strip over the chat, with the task trail on its own row beneath
pr: 653
surface: [chat]
invalidates:
  - "The frame's first row was ONE pinned row that both bars shared: a conversation drew a single dim crumb (`main`, with `▾` after it where the switcher had somewhere to go) and a task page drew its trail, its state and its ✕ on that same line. The frame now pins TWO rows — the tab strip first, the room's own header under it — and a conversation draws no header row at all. Anything reading the room header, the breadcrumbs or the ✕ at frame row 0 is stale: [app.roomHeadRow] is the row number now, and it is the tab strip's height."
  - "The conversation's own name and the picker were reached by pressing that single crumb (`crumbSwitch`). That crumb kind is gone. The name is the lit tab and the picker is the control at the strip's right end."
  - "Over a shared engine handle (`Options.SharedAgent`) the keeper dropped the whole aside on the way out, so a switch left the unsent sentence to whatever the draft debounce had last written. [app.stow] now writes the composer down under the conversation's own identity before it returns, in both worlds."
---

The first row of the chat frame is the conversations this window has been in,
drawn as tabs in the order it FIRST entered them — not in switch order, so a tab
does not move out from under a finger. The current chat has a softly highlighted, bracketed tab; the other tabs have
padded click targets and quieter text. The brackets also work with color disabled. No borders, no ✕ on a tab, no
`+` at the end: closing is `ctrl+w` from the switcher and opening is `/new`, and a
control that could not work on every tab is not drawn at all.

Clicking a tab switches through the keeper's own doors — `bringForward` for a
conversation this process holds, `hopStart`'s two refusals for one it does not —
so the mouse journey and `ctrl+k`'s journey are one implementation. Clicking the
tab already in front comes back out of a task page and does nothing on the
conversation itself. The `▾` at the right end opens the same card `ctrl+k` opens;
where the strip is too narrow it becomes a count (`…3`) of the tabs it could not
spell, which opens that same card — and is drawn inert where the card cannot open,
exactly as the trail's own fold is when everything it hides is inert.

**A tab claims navigation, not a running session.** Over an engine-backed door the
far side holds one conversation at a time and ends the previous one on the swap;
the strip says which conversations this window has been in and can return to, which
is true in both worlds. The existing `closed · <name> — a connection holds one
conversation at a time` notice is unchanged.

The breadcrumb trail, the state word and the ✕ keep the room header, which now
draws on the row beneath the strip. A conversation has no header row: `main` with
nothing after it is the tab above it said twice. Kin rows still hang under the
header, so a task page inside a family is three pinned rows and a flat one is two.
The strip stands down on the two floors the conversation's bar stood down on —
under `roomHeadFloor` columns, or with no breathing row above the draft.

Nothing on the strip reads the disk or the wire on a frame: the tabs come from the
keeper's map, the previous-stack and the surface's own title, the front
conversation's canonical key is memoised against the file it was taken from, and
the drawn line is memoised on width, ink, hover, picker availability and complete tab identities. The list is capped
at 8, and the tab that falls off is the one nobody has been in for longest.

Shared-handle switches also retain the local draft store and restore only the
incoming owner's slots. The outgoing plain-text export cannot become a new chat's
draft. Round-trip coverage pins both chats' text and caret positions. The caret
also survives ordinary local keeper switches.
