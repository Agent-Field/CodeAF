---
kind: fixed
title: A computer that only opened codeaf can pair, and the local relay stops rebuilding in a loop
pr: 2540
surface: [chat, engine, docs]
invalidates:
  - "Opening codeaf once and quitting left an empty conversation that made codeaf pair refuse with 'this computer has local work'; a computer with nothing but that empty conversation now pairs by code or link without --replace, while a sent turn, saved memories, settings or any staged or committed file still refuse."
  - "npm run dev for the hosted relay could rebuild and reload itself in a loop, because its build hook rewrote src/raft/core.wasm inside the watched folder, and its Node process grew to about 9 GB before aborting; an unchanged build now leaves core.wasm untouched and the local wrapper bounds Node's heap."
---
A computer that has only been started holds nothing to lose, so it joins
without being told to replace. Files staged or committed in its own repository,
including ones whose names are only spaces, still count as work.

The local relay's build hook builds the reactor beside the tree and moves it
into place only when its bytes differ, and `npm run dev` caps Node's heap at
2 GB unless the caller sets its own limit.
