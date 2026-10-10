# Desktop place rail rows

## What do the square and dot beside a desktop place mean?

The desktop place rail shows each place as a 32px row. The square on the left
is the place's tint, identifying the place. The muted parent name follows
` · ` when a parent is known. The selected place has a soft raised fill.

A six-pixel dot on the right means something needs you (amber) or failed (red).
Hovering the dot names the attention reported by the engine. Running work
alone draws no status dot. Unknown counts draw nothing.

## How do I close a desktop place from its rail row?

On an Open place row, hovering with a mouse or focusing with the keyboard
reveals the close button. Its tooltip names the place and, when known and
positive, the number of tabs: `Close Marketing · 4 tabs`. The current place's
close shortcut appears beside the text. Pinned rows have no close button.

Touch does not show the hover close button. Use the place row's action menu
and choose Close. Closing uses the existing place-close action; it does not
stop running engine work.

## Why does a closed desktop place say closed · still running?

A place closed while its work is still running or needs you stays in Open.
Its row is muted and says `closed · still running`. It retains its attention
mark and has no hover close button. Selecting the row uses the same Go to
action as an open place.

## Can I use the keyboard to focus desktop place rail rows?

The selected place exposes its current-page state to assistive technology.
Keyboard focus draws a two-pixel accent ring and a four-pixel soft halo,
including on the selected row. Mouse selection keeps the soft fill without
the keyboard ring. Long place names fade under a mask at the row edge.
