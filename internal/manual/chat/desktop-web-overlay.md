# Web page under a menu

## Why the web page went blank when I opened a menu

A web page in the desktop app is drawn in its own view, on top of the window.
A menu, a popover, Go to, Quick Look, the tab overview, a hover
preview or a toast that covers that page would end up underneath it, so codeaf
hides the page while any of those is over it. Dragging a tab or a group hides
it for the drag, so the drop targets stay visible.

The page's area is left blank. There is no line saying the page is hidden.
The address row stays. The page comes back when the last of those closes.

A menu that does not cover the page leaves the page where it is. Closing the
tab while a menu is open closes the page; it does not come back when the menu
closes.

## The web page disappeared behind a menu

The page is still there. It is hidden so the menu, preview, palette, Quick
Look, overview or toast can sit on top of it. Dismiss that and the page
returns, unless you closed the tab while it was hidden.
