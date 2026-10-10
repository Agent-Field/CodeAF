# Desktop shell workspace

## Why does the desktop suggest grouping tabs from the same folder?

When at least three ungrouped tabs refer to conversations in the same recorded
workspace folder, the desktop shows a suggestion at the top centre of the content
card. Group puts those tabs in one named group. Dismiss hides that set for this app
session. A missing workspace folder produces no suggestion. Opening an overview,
rename dialog or tab switcher hides the pill while that overlay is open.

## How do I undo a desktop tab change without undoing my typing?

Command+Z on macOS or Control+Z on Linux undoes the latest structural change in
this window, up to twenty steps. Editors and terminals keep their own text undo.
The overview permits structural undo; another open dialog keeps the shortcut.
Undo preserves newer words and refuses to replace tabs changed since the action.

## Do two desktop windows share my active tab or scroll position?

Windows on the same place share tabs, groups and the recently closed list. Each
window keeps its active tab, recent selection, overview and undo history locally.
Scroll positions are saved separately for each window and restored when content
is ready; moving or typing interrupts restoration. Invalid saved workspace entries
start a fresh workspace. A malformed optional view field is dropped by itself.
