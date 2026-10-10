# The desktop @ picker

## Typing @ in the desktop composer opens the file picker

In the desktop app, type `@` in a conversation's message box and keep typing a
name. A list opens above the composer with matching files from that conversation's
workspace. Each row shows the file icon, the file name, and the folder in a
quieter ink. The folder keeps its start and its end, and the middle gives way
when the row runs out of room. A file at the workspace root shows no folder.

Arrow up and arrow down move through the rows and stop at the ends. Enter or
Tab replaces the `@` and the letters after it with that file's
workspace-relative path. The `@` does not stay. The caret sits just after the
path, and the words around it stay as you wrote them. Click a row to do the
same. The message stays ordinary text: the path is not a chip, and sending
sends that path as text.

Escape closes the list and leaves the caret in the message. The list also
closes when the page scrolls. Shift+Enter still inserts a newline.

The list uses the menu's paper: a soft surface, a shadow, rounded corners, and
no border. A row takes a soft fill on hover and on the highlighted row.
Pressing a row darkens that fill. The words do not change colour.

## The desktop file picker shows nothing

A bare `@` draws no list. A name that matches nothing draws no list. When the
engine cannot answer, the list draws nothing and says nothing. A new chat that
has not been sent yet has nothing to search, so `@` draws nothing there either,
including on Home. An `@` inside a word, such as an email address, is not a
file reference and does not open the list.

The search waits a moment, then asks the conversation for matches. Enter or Tab
before any row is up does what it usually does, and does not guess a path.
While rows are up, Enter and Tab insert the highlighted path and do not send
the message.
