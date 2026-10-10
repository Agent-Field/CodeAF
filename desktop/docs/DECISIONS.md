# Desktop decisions

Append one dated section per task. Leave every other section as it is.

## t-d5-be-files-write-decision (2026-10-10)

### Question

Should the desktop file tab be able to write, rename, or delete a file? (BE-FILE-07, code-bridge §6.5.)

### Call

No. The app ships no file-mutation route and no control for one.

### Why

Shell 3e is a viewer: changes first, a toggle to the full file, and a handoff to your editor. The drawn controls are Changes, File, and Open in editor. Iteration 2 (I2.1–I2.14) does not add an editor. Rename in Shell 3g is the tab and group menu, not a filesystem rename.

The bridge file doors are reads: file bytes, text, stat, list, find, locate, changes, and diff. Open in editor starts a program the engine just listed. It does not change the file. There is no write, rename, delete, or move route. The file header offers the view toggle and that handoff. The lines are text, not a field.

### What still changes files

A conversation can still create, overwrite, edit, rename, move, and delete files through its own tools when you ask. That is not a control on the file tab, and this decision does not remove it. Sending a picture or a file with a message attaches it to that message. It does not write a workspace path from the tab. Writing into a terminal is the terminal, not the file tab.

### What ships

Nothing new is built. A request to write, rename, delete, or move a file through the desktop bridge is an unknown action. The tab does not draw a save, a rename, or a delete, and it does not invent a dirty or saved state. The assumption is also row BE-FILE-07 in the Open table of DESIGN-QUESTIONS.md, so a later design that draws an editor can replace it.
