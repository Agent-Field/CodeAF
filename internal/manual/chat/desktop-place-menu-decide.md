# Desktop place menu: Always ask me and Decision confidence

## Always ask me — stop a place from deciding for me

The place menu has an `Always ask me` entry, on a place tile, on the ⋯ beside a place's name on its Home, and on the Home tab. It is a checked toggle: ticked, that place sends every question to you and decides nothing itself, and its Home status line says `Always asks you`. Choosing it again unticks it and the place decides by confidence again. A place with no choice of its own follows the nearest place above it, and the tick shows what applies. Each change shows a toast with Undo, shared with the other place changes. The entry is absent on archived places, while the app is offline or loading, and when the engine has not said how the place decides.

## Decision confidence — how sure a place must be before it decides

`Decision confidence…` sits under the same menu as `Always ask me`. It opens a list of 50%, 60%, 70%, 80%, 90%, 95% and 100%, with the figure that applies ticked. The default is 90%: a place decides on its own only when it is at least that sure, and otherwise asks you. A higher figure means more questions come to you. Choosing a figure keeps the Always ask me setting as it was, and shows a toast with Undo. Irreversible actions always come to you, whatever the figure. The list offers 50 to 100; a figure the engine already holds outside the list is shown ticked as well.
