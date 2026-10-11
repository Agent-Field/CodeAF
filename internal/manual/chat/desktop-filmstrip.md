# Desktop tab overview previews

## Why are All tabs filmstrip previews faster now?

All tabs opens the grid or Filmstrip from the content the desktop already knows.
Conversation and task previews use the latest reply summary, question or live
command. The filmstrip keeps the half-scale pane and the tab's draft, without
attaching another conversation or loading its full message history.

File, diff and terminal previews keep a small in-memory summary: the first file
lines, first changes or last terminal lines. Already-read content appears when
the overview opens. If the session is already attached but the content has not
been read, the preview fills in after one read. Cards outside the scroll viewport
wait until visible. Unknown content draws nothing.

## Does reopening the desktop filmstrip fetch every tab again?

Closing All tabs keeps preview content in memory. Reopening the grid or Filmstrip
reuses it. An updated conversation timestamp refreshes a resource preview in the
background; the last known content remains visible while it reads. Workspace file
reads and terminal output update the small preview summary too.

The cache holds up to 256 resources and metadata entries per window. It is not
saved to renderer storage. Each cached output line keeps at most 4096 characters.
Reloading the window clears it, and an evicted resource may need another read. Opening a card returns to the real tab, where the full
conversation, file or terminal remains available.
