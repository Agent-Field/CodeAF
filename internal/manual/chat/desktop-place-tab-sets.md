# Desktop place tab sets

## Are my desktop window tab sets saved when I switch places?

Each window shows the saved tab set of its current place. Switching places replaces the strip with that place's tabs; the set you left is kept. Returning restores its tabs and drafts. Switching places does not stop running work.

The engine keeps tabs, groups, pins, splits and closed tabs for each place. Windows on the same place share that set, while which tab each window focuses stays local to that window.

## Why does Now have no pinned Home tab?

Now starts with a quiet conversation and has no place Home. A place has its own pinned Home at index zero, even when loading saved tabs from the engine. Going to a place focuses Home. Reloading keeps the saved focus when that tab still exists. All places is a separate view that can be opened in a strip.

## Where does a chat started from the Home composer open?

Sending from Home starts a conversation in that place and opens its tab after Home, respecting the strip's pinned-tab ordering. Home stays a Home. Enter focuses the new conversation; Command Enter on macOS or Control Enter elsewhere opens it in the background.
