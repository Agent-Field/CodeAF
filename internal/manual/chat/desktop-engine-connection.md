# Desktop engine connection

## Where the desktop engine runs

Settings has an Engine section that says where the engine runs. In the desktop app, a connection on this computer says "On this Mac". An engine on another computer says that computer's host name. The browser build, the one used while developing the app, shows the dev transport address instead: the address the page's engine requests are sent to. It does not show a token.

## What Settings says when the engine is connected

When the engine answers, the Engine section says "Connected". There is no Retry button while it is connected. The words stay the ordinary muted colour. Nothing in this section turns red.

## Reconnecting while Settings tries to reach the engine

While Settings is asking the engine, including just after Retry, the Engine section says "Reconnecting…". Retry is hidden during that ask. The line is not red.

## Can't reach the engine, and Retry

When the engine does not answer, the Engine section says "Can't reach the engine" and shows Retry. Retry is quiet, and it is the only control for this. Pressing it asks again. If the engine still does not answer, the same line and Retry stay. If it answers, the section says "Connected" and Retry goes away. Nothing turns red.
