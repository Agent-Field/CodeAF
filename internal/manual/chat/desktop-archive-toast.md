# Desktop archive notification

## Why did the desktop show Archived tabs idle for more than 12h?

At launch the desktop archives idle conversation tabs after twelve hours, except
active, pinned, running or waiting tabs. Nothing is deleted. One shared
notification reads “Archived N tabs idle for more than 12h” (or “Archived 1 tab
idle for more than 12h”). Review opens History; Restore all reopens those tabs
in the background and removes their archive marks.

## How long does the archive notification stay? Can I dismiss it with Escape?

The archive notification uses the window’s shared toast. It leaves after six
seconds, pausing while hovered or while a button inside it has keyboard focus.
Moving focus from Review to Restore all keeps the clock paused. Escape dismisses
it when no dialog is open; dismissal leaves the tabs archived. A newer window
notification replaces it. Archived conversations remain available in History.
