# Desktop file chips

## How do I open a file chip in an editor?

Right-click a file chip in the conversation and choose "Open in editor".
That asks the desktop to open the file's absolute path. "Reveal in Finder"
on a Mac, or "Reveal in Files" elsewhere, shows that same absolute path in
the file manager. Both stay available while the file is in this workspace.
With the chip focused, Shift+F10 opens the same menu. Down Arrow moves
through it, Enter chooses, and Escape closes it and puts focus back on the
chip. A plain click still opens the preview, not the editor.

## How do I copy a file chip path?

Right-click the file chip and choose "Copy path" or "Copy relative path".
Copy path copies the absolute path. Copy relative path copies the path
inside the workspace, the same path the chip was given. Copy relative path
is unavailable when the file is outside the workspace. The keyboard path is
the same menu: focus the chip, press Shift+F10, move to the item, and press
Enter. Copying does not open the file.

## How do I see the full path on a file chip?

The chip draws the file name and a shortened parent directory. Hover it for
500 milliseconds and the shared tooltip shows the chip's own full path, not
a native title and not only the file name. A missing file adds " · not found".
A file outside this workspace adds " · outside this workspace". Keyboard
focus shows the same tooltip. Pressing, leaving the chip, or Escape puts it
away. Touch does not show it.
