---
kind: fixed
title: Taking a conversation from the switcher leaves the place you took it from
pr: 1040
surface: [chat, docs]
invalidates:
  - "`alt+k` (`ctrl+k` when this branch was written) then `enter` on HOME (or any other place) used to leave the place standing. It did switch — the conversation behind the screen changed — so the keystroke read as doing nothing while it had quietly swapped what home was drawn over. `hopTake` now ends in `hopLand`, which comes down off whatever place is showing, so the card's `enter open` lands you in the conversation on every screen it opens over. The same goes for `ctrl+tab` quick switching and for a row taken from below the fold."
  - "A refusal from the card (`that conversation is no longer open`, a locked conversation, a workspace that has gone) used to be said with `app.note`, onto the entry line of a conversation nobody could see while a place was up. It now goes through `app.hopSay`: home's own sentence, a place's `app.pageMsg`, or the conversation's entry line when nothing is standing over it. A refused row still leaves the place up, on purpose — the person has to be able to read it."
---

The switcher is drawn over the screen rather than being a screen of its own, which
is what lets it open on all seven places. The cost was that taking a row moved the
conversation UNDERNEATH the place and left the place in front, and nothing in the
card said so.

Every other door between conversations already ended with the place coming down and
spelled it for itself — home's `enter` in `closeHome`, the search place's row door
in `standDownFullscreen`, `ctrl+shift+t` in `closeHome` again. `hopLand` is that
statement made once, for the one door that can be opened from anywhere, which is why
it asks `pageShowing` rather than naming home.

`ctrl+w` on the card is deliberately NOT changed: closing a tab from home leaves you
on home, because that key is about what is on the tab row and not about where you
are standing.
