# Desktop scroll memory

## Does reloading the desktop keep my position in a conversation?

The desktop remembers each tab pane's scroll position. Returning to a tab or
reloading the window restores where you were reading, after the transcript has
loaded. A conversation left at the bottom returns to the bottom, including new
content. A conversation opened for the first time follows the latest reply.

Scroll positions are saved at most once every 500 milliseconds during scrolling;
leaving the window flushes pending changes. If you scroll or press a key while a
restoration is waiting, your action takes priority. Blocked or full local storage
can prevent positions from surviving a reload.

## Do two desktop windows share my scroll position?

Each window remembers its own position in each pane. Scrolling in another window
showing the same conversation does not move this window. Scroll positions are
kept locally and are never synced through the shared workspace store.
