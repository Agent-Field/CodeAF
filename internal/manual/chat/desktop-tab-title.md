# Desktop tab title

## Where is the full title when a desktop tab name is cut off

In the desktop app, a tab's name fades out over its last stretch instead of ending in an ellipsis. When that name does not fit — the text is wider than the tab, or it runs into the fade — the full title is not lost.

Hover the tab, or move to it with the keyboard. If it is the tab you are in, or it is one of the narrow icon-only tabs, a tooltip shows the full title. An inactive tab does not use that tooltip: hovering it opens a preview card, and the card already shows the full title. A tooltip never opens while a preview card is open.

A pinned tab you are in is icon-only, so the tooltip is the only place its name is written. A pinned tab you are not in uses the preview card instead.

A name that fits on the tab, clear of the fade, has no tooltip. The words on the tab are the whole name.

## How long is the desktop tab full title tooltip

On a desktop tab whose name is cut off, the tooltip waits half a second (500ms) after you hover, then shows the full title and nothing else. It uses the same wait as the hover preview. Once one tooltip has shown, the next one along the strip opens at once while the pointer keeps moving.

It goes away when you leave the tab, press it, or press Escape. A tap does not open it: touch is not a hover. Keyboard focus does open it, after the same wait, and only when you reached the tab from the keyboard.

The tab's own name for a screen reader stays the title. The tooltip does not repeat it.

## What does a pressed desktop tab look like

Holding a desktop tab down darkens the whole chip for a moment (80ms) and leaves the words the same colour. This is the press, separate from the full-title tooltip. Releasing it returns the tab to the fill it had: the open tab's paper, or the hover fill. Colour stays on the small mark only, when a tab needs you or failed.

Holding the close mark does not darken the chip. That mark has its own press.
