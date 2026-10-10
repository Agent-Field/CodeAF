# Desktop Home knowledge list

## How do I edit or remove what Marketing knows on Home?

The desktop knowledge-list component shows “What Marketing knows” with a count when
there are recorded lines. Each line shows its real source when known. Hover or focus
reveals the pencil and remove controls. The pencil opens a text field: Enter saves
and Escape cancels. Focus a row and press Delete to remove it; the shared toast
provides Undo after the engine accepts the removal. Delete inside an editing field
only edits the text. Failed saves keep the draft and show the engine's error.

This component requires the Home owner to supply stored lines and engine write
callbacks. It does not read or write knowledge by itself. Actions without a wired
callback are absent, and read-only mode has no mutation controls.

## How do I add something Marketing should know?

When adding is wired, the field says “Add something Marketing should know”. Type a
line and press Enter. Blank lines are ignored. The field clears after a successful
save; a refusal keeps what you typed. Manually written lines show “you added”.

Home shows the first three lines from the knowledge projection. All expands the
complete list in place; choosing All again returns to the first three. Unknown or
zero counts are omitted.

## Why is a knowledge line crossed out or asking still true?

A replaced line is struck through and its known replacement date reads, for example,
“Replaced Tue · kept for 7 days”. It leaves the list after seven days. An unused
line aged sixty days shows “still true?” with Yes and Remove when those actions
are wired. Yes records confirmation through the owner; Remove uses the same removal
and Undo as the row action. Recorded confirmations suppress another prompt for
sixty days. Missing dates are never guessed.
