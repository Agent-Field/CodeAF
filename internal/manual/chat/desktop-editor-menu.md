# Desktop editor menu

## How do I open a desktop file in an editor?

A file tab's “Open in” menu lists the editors reported by the engine, with the default first and a muted “default” label. Choose an editor to open that file on the engine machine. With exactly one local editor, a displayable file keeps the “Open in editor” button instead. A file that cannot be displayed keeps the menu.

The menu ends with “Copy path” and “Copy relative path”. Copy path uses the full path when the engine supplies it; Copy relative path copies the file's workspace-relative path. Command+Shift+C on macOS (Control+Shift+C elsewhere) copies the focused file's path.

## Why does a remote engine only offer copy path in the desktop editor menu?

A remote engine's “Open in” menu offers only “Copy path” and “Copy relative path”; it does not launch an editor. No discovered local editors also leaves only these copy actions.

## How do I use the desktop editor menu with the keyboard?

Focus “Open in” and press Enter to open its menu. Down Arrow moves to the next item, Enter selects it, and Escape closes the menu and returns focus to “Open in”.
