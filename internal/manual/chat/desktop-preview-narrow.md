# Desktop preview on a narrow window

## Does the tab preview stay on screen on a small window

Hovering an inactive tab still opens its preview when the desktop window is 600px wide or narrower. The card stays 8px inside the window edge. It is at most 300px wide, and never wider than the window minus 16px, so it does not run off the side. On a wider window the card keeps its usual size: a 300px column with 14px of padding on each side, still 8px clear of the edge when it would otherwise be cut off.

## Why is there no preview when I touch a tab

A screen that cannot hover does not open a preview card. Touching a tab selects it. Keyboard focus on that kind of screen does not open a card either. Nothing is drawn in place of the card. A card that was already open closes when the screen reports that it cannot hover.

## How wide is the hover preview on a narrow window

From 600px down, the hover preview's width is the smaller of 300px and the window width minus 16px. That 16px is the 8px clearance on the left plus the 8px clearance on the right. The title, the draft and the actions stay inside that card.
