---
kind: fixed
title: A conversation that has said nothing is called new conversation on the tasks place, not by its id
pr: 915
surface: [chat]
invalidates:
  - "The tasks place drew a conversation with no title as its raw session id, capitalised — `De9ea39e6f4c18c3` where the id is `de9ea39e6f4c18c3`. No longer true: the row answers `new conversation`, the same word home's column spells, and no frame of that place contains a bare sixteen-hex-digit id."
  - "The tasks place's row composer read `row.Title` directly, so the titleless row inherited whatever fill the world scan left in it. No longer true: a title that equals the row's own id, folded, is discarded and the row is named by [unnamedConversationWord] — one constant, already behind home's own row, rather than a second spelling of the word."
---

The launch's own first session exists before the title role has had anything to
name, and #905's rewrite of the table's row composer lost the fallback the list
had before it: the row drew `De9ea39e6f4c18c3` — a machine's word in the one
column whose whole job is matching names, and title-cased into the bargain, so it
read as a name somebody chose. A person scanning for their own conversation had
to learn that the capitalised thing was not one.

The composer now keys on the trimmed title equal to the row's own id, folded,
because a titleless row's Title is never empty — `homeName` fills it with the
title-cased id, so a byte-exact or emptiness check never fires. Only the name
changes: the row keeps its place, its age stays empty (lastUserAt is the zero
time and the emptiness law draws nothing), and `chatProjectWord` still sees the
row, so a project that is not this name does not echo the word back as a tag.
