# Desktop Home sources

## Where are the files, folders and URLs used by a desktop place?

Open the place's Home. Sources lists the place's own folders, repositories,
files, URLs and saved chats supplied by the engine. Each row names the source
and its kind. Known missing or unreadable sources say so in quiet text;
unknown availability has no extra status. Sources is hidden when the list
is empty. Inherited context is separate from this list.

## How do I remove a source from desktop Home and undo it?

Hover a source row or focus its Remove button with the keyboard, then press
Remove. On touch the action stays visible. Removal changes the place's context;
it does not delete the original file or folder. The row remains until the
engine accepts the change. While the write is pending, further source changes
are disabled. If removal fails, the source stays and the error appears below
the list. Press Remove again to retry.

A successful removal uses the window's shared structural Undo: press Undo in
the notification, or Command+Z on macOS / Control+Z elsewhere outside a text
field. The window keeps up to twenty structural actions. If later changes
prevent reversal, codeaf reports the engine's refusal. Offline Home lists
cached sources without Remove or Add actions.
