# Opening a path in the desktop app

## That file is outside the workspace

Open in editor, Reveal in Finder and Reveal in Files ask the engine which directories this computer may open. The window does not get to name a workspace. A file is opened only when it sits inside one of those directories. Anything else is refused with "That file is outside the workspace". A shortcut is followed before that check, so a link whose target leaves those directories gets the same sentence. When the engine's list is empty, every path gets that sentence, and the desktop does not look at the disk first.

## That file no longer exists

When the engine has named directories and the path is absolute but nothing is there anymore, Open in editor and Reveal say "That file no longer exists". A path that is not absolute is refused with "That file is outside the workspace", because only an absolute path can be checked against the engine's directories.

## The workspace is unavailable

When the desktop has not yet heard the engine's connection, or the directory list cannot be read, Open in editor and Reveal say "The workspace is unavailable". They do not open the path anyway.
