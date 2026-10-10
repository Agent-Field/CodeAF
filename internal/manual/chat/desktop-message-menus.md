# Desktop message menus

## Right-click my message to copy it, or edit a sent message

Right-click a user bubble and choose **Copy**. It copies the same literal text as
its hover **Copy message** button, including line breaks. Sent messages cannot
be edited. Embedded pasted-text cards are not included in either copy action.
An attachment-only bubble has Copy disabled because there are no words to copy.

## Copy the full answer from a folded turn

Right-click a folded turn and choose **Copy answer**. It copies all answer blocks
in order as Markdown source, separated by blank lines. It does not copy the
one-line digest, interim updates or tool logs. With no answer, Copy answer is
disabled. Copying leaves the turn folded.

## Open a folded turn in a new tab

The folded turn's **Open in new tab** opens another view of the same saved
conversation in the background, keeping the current tab selected. It passes the
turn ID as the requested scroll anchor. The current shell does not yet apply
that anchor, so the new view opens at the conversation's end. An unsaved view
without a tab-opening handler does not offer this action.

## Shift+F10 or the ContextMenu key on a message or folded turn

Focus a user bubble or folded turn, then press **Shift+F10** or the
**ContextMenu** key to open its menu. Arrow keys move through the choices,
Enter selects, and Escape dismisses the menu and returns focus to its trigger.
