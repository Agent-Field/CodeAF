---
kind: fixed
title: the @ list finds recent conversations on every box without taking over prose
pr: 1725
surface: [chat, docs]
invalidates:
  - "The `@` list's recent conversations were read once per process, so a conversation started in another window after the first `@` was never on it. They are read again every time the list opens."
  - "`@chat:` and `@team:` kept the first eight rows of their section and showed no sign of more. A prefixed list keeps up to thirty-two and scrolls; the bare `@` still keeps eight per section."
  - "The manual did not say which conversations the `@` list holds. It does: every open tab except the one you are in, then the twenty most recent in this project; open tabs from other projects are included too; older or other-project saved conversations use `/resume` unless already open here."
  - "On the new-chat page (`+`), the `@` list left off the conversation the window came from, as though you were typing inside it, so `@chat:kim` beside a lit `tell me about kim jung il` said `no conversation matches`. The start page leaves no conversation off."
  - "A prefixed `@chat:who is` search closed at its first space. `@team:`, `@chat:` and `@file:` now hold up to three spaces and match every word in any order on teams, conversations and files. A bare `@` still ends at its first space, so ordinary prose never reopens the list. A multi-word search with no match closes only after its catalog has been read."
  - "Home's `@` list offered files alone, and `@chat:` typed there answered `no file matches`. Home's list has the same teams and conversations sections and the same `@team:`, `@chat:` and `@file:` prefixes as a conversation's box, and leaves no conversation off."
  - "Home's `@` list could crash on dev too: opening replaced the project row that supplied its folder, then the loader changed roots and cleared paths after rows were ranked against them. Home captures the folder before the edit and rebuilds rows with every catalog change; answers from an older target are ignored."
  - "A pasted opening on home skipped the catalogs and reads, and an initial spaced search could close before any read started. Keys and pastes now populate the same catalogs and start the same reads, keeping unread searches open."
  - "Recent rows or a file walk could move the chosen row before Enter, and home's arrows left its completion cursor behind. Data arrivals now follow the chosen row while it remains offered."
  - "Recent-row canonicalization walked the disk on the update loop. Keys now travel with the off-loop read, preserving symlink deduplication and the hosted cleaned-path rule."
  - "Punctuation after a chosen mention reopened an empty list, and home's chosen teams were drawn plain. Punctuation keeps the list closed and both home widths draw the team's colour."
---

Santosh's report (2026-09-30): a tab reading `cloudfl…` was on the strip and
`@chat:cloudfl` did not list it. The match itself was fine; what the list held was
not. The recent list was a snapshot taken on the window's first `@` and kept for
the life of the process, and a section cut at eight rows said nothing about the
rest. The conversation in front is still left off on purpose: pointing at the
chat you are typing in is not a reference.
