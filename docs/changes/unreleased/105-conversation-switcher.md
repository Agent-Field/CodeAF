---
kind: added
title: ctrl+k is the switcher, and the task page folds a family under its root
pr: 105
surface: [chat]
invalidates:
  - "`ctrl+k` was bound in exactly one place — saving a harness design from inside its room — and the keys page said so outright: `Not bound anywhere else`. It is now the conversation switcher everywhere; the design room's approval row is modal and still takes the key first while it is up."
  - "`tab` on an empty box was the ONLY way between conversations, and it reaches exactly one of them. It still does that, unchanged, but it is no longer the whole story: `ctrl+k` opens a card of all eight."
  - "The legend above the box read `space space home · tab last · / commands` whenever a second conversation was open. With three or more open the middle clause is now `ctrl+k switch`."
  - "The tasks place (`ctrl+.`) drew every row flat, so a run that split into eight workers arrived as eight peers of everything else. Families are now folded under their root, shut, with `+N under` on the root row; `→` opens one and `←` shuts it. Nothing about the roster column changed — this is the page catching up with it."
  - "A place's map (`alt+.`) named four chords. It names a fifth, `ctrl+k switch conversation`, wherever the switcher would act."
---

The keeper has held eight conversations alive since the conversations wave, and
the only gesture between them was `tab`, which goes to one and says nothing about
the other six — so reaching a third meant leaving the chat for home, reading a
list, and coming back. That is a screen transition for something people do fifty
times a day.

The card is built entirely from what this process already holds: the keeper's map,
the sidecar each detach left, and three predicates asked of agent pointers that
are already in memory. No disk, no world scan, no presence read, and nothing over
the wire on a hosted session — which is what lets it appear between two
keystrokes. Every row says what CHANGED since you last looked (`asking you
something`, `3 tasks running`, `it finished while you were away`, `nothing new`)
rather than what the conversation is, so the switcher doubles as the catch-up.

`ctrl+k` rather than `ctrl+tab` because `ctrl+tab` has no legacy encoding: it
arrives as a bare `tab` unless the terminal speaks the kitty keyboard protocol,
and WezTerm and Windows Terminal both spend it on their own tabs by default. It
is bound as an alias where the terminal answers the keyboard query and is never
named where it would not be delivered — the law `ctrl+1`…`ctrl+7` already
follows. `alt+tab` belongs to the window manager and was never available.
