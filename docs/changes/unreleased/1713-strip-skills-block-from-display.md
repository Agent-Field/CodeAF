---
kind: fixed
title: the skills block no longer prints as part of a person's message
pr: 1713
surface: [chat]
invalidates:
  - "#1504 was believed to have fixed `Skills suited to this message:` printing at the top of a conversation and after messages. It proved the journal and store keep only the typed words — which they do — but the engine's in-memory transcript still carried the block, and every display door (Transcript, AttachReplay, and the replay that redraws an opened conversation) drew it straight from there. The display layer now strips the block by injection provenance, so the record a surface draws is the person's words only."
---
`Skills suited to this message:` is per-turn skill selection, context for the model.
`attachTurnSkillsLocked` splices it onto the copy the provider reads in
`a.messages`, and that array is also what `shapeEntries` walks to build every
`DisplayEntry`, so replaying or reopening a conversation drew the block as
though the person had typed it. The strip lives in `shapeEntries` — the one place
a message becomes a displayed row — and it reads PROVENANCE, never the text:
`attachTurnSkillsLocked` marks the one message it spliced with the exact bytes
it appended (a memory-only `messagePresentation` mark; the journal keeps only
the typed words, so a restored message carries no mark), and the display takes
off exactly those bytes from exactly that message. The model-bound copy is
untouched, and a person who pastes a whole skills block into a message of their
own keeps every word — suffix matching could not tell the two apart, and this
can.
