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
  - "The switcher lists EVERY conversation on this machine, not only the ones this terminal has open: the open ones above a rule, the rest below it. Taking a row below the rule opens it beside the one you are in, which is what `enter` on home already did. So the key acts on a session that has just started, where nothing but the current conversation is open."
---

The keeper has held eight conversations alive since the conversations wave, and
the only gesture between them was `tab`, which goes to one and says nothing about
the other six — so reaching a third meant leaving the chat for home, reading a
list, and coming back. That is a screen transition for something people do fifty
times a day.

The open half of the card is built entirely from what this process already holds
— the keeper's map, the sidecar each detach left, and three predicates asked of
agent pointers already in memory. The rest of the machine is one world reading,
taken on the keystroke and never on a frame, which is affordable because this
gesture REPLACES pressing `space space`: it takes the same reading and then
draws a whole page with it.

Every row says what CHANGED since you last looked (`asking you something`,
`3 tasks running`, `it finished while you were away`, `nothing new`) rather than
what the conversation is, so the switcher doubles as the catch-up.

The first cut listed only what was already open, and that was the defect: a
person who has just started aforge holds one conversation, so the key did
nothing, was advertised nowhere, and could only be found by somebody who already
knew that `enter` on home opens a second one. The feature was invisible until you
had learned the thing it exists for.

`ctrl+k` rather than `ctrl+tab` because `ctrl+tab` has no legacy encoding: it
arrives as a bare `tab` unless the terminal speaks the kitty keyboard protocol,
and WezTerm and Windows Terminal both spend it on their own tabs by default. It
is bound as an alias where the terminal answers the keyboard query and is never
named where it would not be delivered — the law `ctrl+1`…`ctrl+7` already
follows. `alt+tab` belongs to the window manager and was never available.
