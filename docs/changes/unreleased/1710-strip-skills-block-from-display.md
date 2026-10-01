---
kind: fixed
title: the skills block no longer prints as part of a person's message
pr: 1710
surface: [chat]
invalidates:
  - "#1504 was believed to have fixed `Skills suited to this message:` printing at the top of a conversation and after messages. It proved the journal and store keep only the typed words — which they do — but the engine's in-memory transcript still carried the block, and every display door (Transcript, AttachReplay, and the replay that redraws an opened conversation) drew it straight from there. The display layer now strips a well-formed trailing block, so the record a surface draws is the person's words only."
---
`Skills suited to this message:` is per-turn skill selection, context for the model.
`attachTurnSkillsLocked` splices it onto the copy the provider reads in
`a.messages`, and that array is also what `shapeEntries` walks to build every
`DisplayEntry`, so replaying or reopening a conversation drew the block as
though the person had typed it. The strip lives in `shapeEntries` — the one place
a message becomes a displayed row — and removes only a whole trailing render the
producer itself would have appended: the lead header, entry lines beginning
`- `, and the closing conflict line, as a suffix of the message. The model-bound
copy is untouched, and a person who pastes the marker or the closing sentence
into their own message keeps every word.
