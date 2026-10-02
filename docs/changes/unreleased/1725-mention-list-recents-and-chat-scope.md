---
kind: fixed
title: the @ list finds recent conversations on every box without taking over prose
pr: 1725
surface: [chat, docs]
invalidates:
  - "The `@` list's recent conversations were read once per process, so a conversation started in another window after the first `@` was never on it. They are read once per opening on every box: a new `@` token, typed or pasted, or the list returning on the next letter after `esc`."
  - "`@chat:` and `@team:` kept the first eight rows of their section and showed no sign of more. A prefixed list keeps up to thirty-two and scrolls; the bare `@` still keeps eight per section."
  - "The manual did not say which conversations the `@` list holds. It does: every open tab except the one you are in and the window's own unnamed, unsent front, then the twenty most recent in this project; open tabs from other projects are included too; older or other-project saved conversations use `/resume` unless already open here."
  - "On the new-chat page (`+`), the `@` list left off the conversation the window came from, as though you were typing inside it, so `@chat:kim` beside a lit `tell me about kim jung il` said `no conversation matches`. The start page leaves no eligible conversation off."
  - "A prefixed `@chat:who is` search closed at its first space. `@team:`, `@chat:` and `@file:` now hold up to three spaces and match every word in any order on teams, conversations and files. A bare `@` still ends at its first space, so ordinary prose never reopens the list. A multi-word search with no match closes only after its catalog has been read."
  - "Home's `@` list offered files alone, and `@chat:` typed there answered `no file matches`. Home's list has the same teams and conversations sections and the same `@team:`, `@chat:` and `@file:` prefixes as a conversation's box, and leaves no eligible conversation off."
  - "Home's `@` list could clear its paths under already-ranked rows and crash, including on dev. It now walks the pinned target or this window's file root, never the row selected before opening. Locally the list, foot and sentence use the same folder; over `--host` the unpinned list walks this machine's folder like a conversation's list, while the foot and send keep the far workspace. Catalogs and rows change together; old-folder answers are ignored; closed-list arrows preserve the completed walk."
  - "A pasted opening could skip the fresh recent read when an earlier catalog matched nothing. A new token now reads recents exactly once, even when its unmatched display closes; letters and caret moves within that opening never restart the read or repeat the opening notice. Dismissing it with `esc` ends the opening on every box; the next letter brings it back with one fresh read and one opening notice. Prefixed multi-word chat searches wait for their token's answer."
  - "Recent rows or a file walk could move the chosen row before Enter, and home's arrows left its completion cursor behind. Data arrivals select the best match unless the person chose a row since the query last changed; that choice survives while still offered. A changed query returns to the best match."
  - "Emptying home's box skipped completion sync and kept its list open, so the next token could miss conversations started in another window. Every edit now syncs the token, including the empty box; the next `@` starts a fresh read."
  - "Recent-row canonicalization walked the disk on the update loop. Keys now travel with the off-loop read, preserving symlink deduplication and the hosted cleaned-path rule."
  - "Punctuation after a chosen mention reopened an empty list, and home could draw chosen teams plain from a stale conversation catalog. Punctuation keeps the list closed; every box draws the current team colour, and plain draft rows allocate nothing for team painting."
  - "An unsent shell could appear on the `@` list under its draft's tab name and supply an empty digest. Only the window's own unnamed front with no user entry or opening prompt is left off for having nothing sent. Every held, restored or side tab is offered without title/opening heuristics, including an untitled conversation whose first user message was a queued follow-up."
---

Santosh's report (2026-09-30): a tab reading `cloudfl…` was on the strip and
`@chat:cloudfl` did not list it. The match itself was fine; what the list held was
not. The recent list was a snapshot taken on the window's first `@` and kept for
the life of the process, and a section cut at eight rows said nothing about the
rest. The conversation in front is still left off on purpose: pointing at the
chat you are typing in is not a reference.
