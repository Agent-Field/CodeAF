# Undoing desktop place actions

## Undo closing a desktop place after its toast disappears

Command Z on Mac, or Ctrl Z on Linux, takes back the newest structural action made in this desktop window. A closed place reappears in the rail through the engine's reopen operation; its saved tabs are preserved. The window keeps showing the place you are looking at now. Dismissing the close toast does not forget that step.

## Undo archiving, moving, deleting or filing in desktop places

Place writes keep the engine's exact undo receipts. Undo restores archive, move, delete and filing changes only while the engine permits their inverses. Deleting a place does not delete its chats. If places changed afterwards, the engine can refuse Undo; codeaf shows its reason and drops that unavailable step. A connection failure keeps the step for an explicit retry.

## Do desktop tabs and places share the same twenty undo steps

Tabs and places use one stack of twenty structural actions per window, newest first. The twenty-first action forgets the oldest. Another window has its own stack. Closing or reloading a window forgets its steps; there is no redo. A toast's Undo consumes its own step, so Command Z cannot repeat it. Draft edits, tab selection and navigation between places do not add steps. In a text field or terminal, the Undo shortcut stays with the editor.
