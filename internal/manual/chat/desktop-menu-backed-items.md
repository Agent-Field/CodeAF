# Tab menu when move and copy have nothing behind them

## Why is Move to new window missing from the tab menu

In the desktop app, right-click a tab and the menu lists Move to new window. Choosing it opens another window on the same place, focused on that tab. The tab stays in this window's strip too. If it was the one on screen and another tab is open, this window shows the neighbour instead. The work keeps running.

A browser has no second window, so Move to new window is not in the menu there. Nothing disabled is drawn in its place.

If the place is one the window door does not open, choosing the item opens no window. The tab stays, and the toast says it is still here.

## Why is Copy link missing

Copy link is in the tab menu only when the tab has something durable to point at: a saved conversation, a task, a file, a diff, a terminal or a web page. The menu shows the shortcut beside it, except on a file or diff tab, where that shortcut stays Copy path.

A new tab, a conversation that was never sent, Settings, History and the Inbox have no Copy link. The row is left off. It is not shown disabled.

## What does the copy link shortcut do when a tab has no link

The shortcut is Command Shift C on a Mac and Control Shift C elsewhere. On a tab with no link it does nothing: nothing is copied and no toast appears. On a file or diff tab, and inside a terminal, that shortcut is not Copy link.

When the tab does have a link, the shortcut copies it and the toast says it copied the link to that tab's title.
