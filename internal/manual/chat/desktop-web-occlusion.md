# Web page under an overlay

## Why is the page title on the web sheet when I open a menu

A web page in the desktop app is drawn in its own view, on top of the window.
A menu, a context menu, a hover preview, a tooltip, a toast, the tab overview,
a drag split zone, Quick Look or a dialog that meets that page would end up
underneath it, so codeaf hides the page while any of those overlaps the sheet.

The sheet keeps its fill. When the page has a title, that title is written on
the sheet in the same muted ink as other secondary text. There is no line
saying the page is hidden. A title the page has not reported yet leaves only
the fill. The address row stays. The page comes back when the last overlay
that covered it closes.

Dragging a tab or a group hides the page for the drag, so the drop targets
stay visible.

## Does a menu that does not cover the web page hide it

No. An overlay that misses the sheet leaves the page where it is. Only a
surface that overlaps the sheet hides that page.

## When does the hidden web page come back

Dismiss the menu, preview, tooltip, toast, overview, drag, Quick Look or
dialog and the page returns. Closing the tab while it is hidden closes the
page; it does not come back when the overlay closes.
