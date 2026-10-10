# Desktop window current place

## Does each desktop window remember its own place when I relaunch?

A desktop window's current place key is Now, All places (root), or a real place id. Its remembered key belongs to that window's label, so changing one window's place does not overwrite another window's remembered place. A destination supplied when opening a window takes precedence over a remembered key.

Remembering the current place does not copy or change a conversation. Tabs and conversations have their own storage. If local storage is unavailable, navigation can still work, but its destination cannot be remembered for relaunch.

## Why does a deleted or archived place fall back to Now?

When a successful places read says the current place was deleted or archived, its current key becomes Now and Now is remembered for that window. All places (root) and Now do not need a matching graph place.

A graph that has not loaded, or a failed engine read, does not prove that a place was deleted. The current key is kept until a successful read can check it. An invalid remembered key starts on Now.
