---
kind: fixed
title: "Five blind pass-through follow-ups: in-place edits move, cards wrap, quits stay honest"
pr: 2886
surface: [chat, engine, remote, docs]
invalidates:
  - "A file edited in place on one computer by something other than a tool call, such as an append to README.md from a shell, was missing from the next saved turn when that turn ran no tool or only tools that named other files, so the computer taking the chat next did not have it and a later take back kept nothing; the engine now compares every file's own size, modified time and mode with the last saved turn's record before it saves, so the edit travels with the next turn, and a save still walks the folder once and reads no file it did not already read."
  - "The continue card and the refusal card kept two rows of their sentence and dropped the rest without a mark, so the refusal for blind pass-through ended at `switch it back on` and the continue card for a big folder at `copies that folder too: 174`; the sentence now wraps over up to six rows and a longer one ends in an ellipsis."
  - "Quitting a window that only showed a chat another computer held could print `codeaf could not save this chat; unsaved turns...` for a failure left by an earlier refused save, although that window had no turns of its own to keep; it now says nothing, while a window with turns of its own that could not be saved still gets the sentence."
  - "The cause of a save that failed at quit was written nowhere, only the sentence a person reads; it is now one line in chat.log, `quit: final save failure (transcript <path>): <error>`, written once."
  - "An engine could die with `fatal error: concurrent map iteration and map write` when its model-timing belief file was rewritten while a model call started, because the snapshot it wrote shared its maps with the live ledger; the snapshot now owns its maps."
  - "With hosted sync storage off, the sync service's object store was still asked whether it held objects, with every object id of a task copy, and a task copy did its work before the upload was refused; sending or taking a task copy is now refused first, with the same storage-off sentence, and the store client's existence check asks nothing while storage is off. A task copy stored while storage was approved can no longer be received once storage is off."
---
Five follow-ups to blind pass-through (#2812). A failed take's one lookup of the
sync service when the computer that has the chat is off is unchanged, and so is
reading frames that were stored before storage closed.

Where the engine looks for an in-place edit is the same walk of the chat's folder
that every save already made after itself for the saved-state record, now made
before the save and used for both, so a save costs no extra walk. The record can
only name what the save could have read: a file edited while a save runs is
noticed again at the next one.
