# Desktop open with a chosen editor

## Desktop Open with — open this file in a chosen editor

In the desktop app, Open in can start one editor the engine just listed for that file. The editor is the id that list returned: a desktop-file name such as `code.desktop`, or a Mac bundle id. codeaf asks that conversation's editor list again when you choose one, and starts it only when the id is still on the list. The id is not a command. Nothing typed into the page is run as a program.

The path has to be absolute, and it has to sit inside a directory the engine says this computer may open. That is the same directory list Open in editor and Reveal already use.

## That editor is not one this machine just listed

When the id is not on the fresh list, opening stops and says "That editor is not one this machine just listed for this file." A string with a space, a slash or a shell mark gets that same sentence. codeaf does not try it as a command.

When the engine is on another machine, opening says "The engine is on another machine." When this machine has no display, it says "This machine has no display, so an editor cannot be opened." When a program cannot be started and the list gave no such sentence, it says "This machine cannot start an editor."

A path outside those directories is refused with "That file is outside the workspace" before any editor is asked. A path that is not absolute gets that same sentence. When the directories are named and the file is gone, it says "That file no longer exists". When the desktop has not heard the engine yet, it says "The workspace is unavailable".

## Open with is missing in the browser

A browser build has no opener. Open in does not offer a chosen editor there. The chosen editor exists only in the desktop app.
