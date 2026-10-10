# Remember something in a desktop place

## Remember this in my place: where does it get saved?

In a desktop chat filed under a place, the `remember` tool saves a knowledge line in that place by default. Its source is `said-in-chat`, with the conversation identifier and time. The chat shows `Saved to <Place>: <text> · Undo`. The line and its Undo action survive transcript replay.

With several direct places, codeaf chooses the first parent-most place and adds `first parent-most place` to the saved line. An inherited ancestor is not a save target unless the chat is also filed directly there. Equal-depth places use the order in which the chat was filed. Archived places are excluded.

An unplaced chat keeps the ordinary memory behavior. Explicit `user`, `project` and `env` scopes still save general memory. Explicit `place` scope refuses an unplaced chat with `this conversation has no place`. Invalid or blank text is refused; a failed save never produces a saved line or Undo action.

## Undo something I asked a chat to remember

Press Undo beside the saved line to remove exactly that knowledge line. Other facts and work saved by another window stay intact. Undo works across the separate chat engine and desktop bridge processes.

A line edited, replaced or otherwise changed after saving cannot be removed by that old Undo action. A line already removed cannot be undone again. The note remains in the transcript as the record of what was saved.
