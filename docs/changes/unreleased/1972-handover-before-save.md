---
kind: fixed
title: A retiring engine no longer holds up the next launch while it saves
pr: 1972
surface: [chat, engine]
invalidates:
  - A host held its workspace lock until every conversation finished saving; it now removes its socket and releases the lock first, and each conversation keeps its own journal lock until its save is done.
  - Retire and engine --stop waited up to 10 s for every journal to be flushed and then failed; they now return once the workspace is free and its saves are done, or after at most one more second, while a slow save finishes in the old process.
  - A launch that found the host lock busy waited only for a socket; it now starts the host itself the moment the holder lets go.
---
When a new build replaced the binary, the old engine retired and saved its
conversations while it still held the workspace. In a large folder that save
runs for up to 30 seconds. A launch arriving during it waited 10 seconds for an
engine nobody was starting, then opened the conversation in its own process,
which took about 15 seconds in the home folder. The old engine now gives the
workspace up before it saves, so the next launch starts the current build at
once. A launch that meets an older engine still saving starts the new one as
soon as the old one lets go.
