---
kind: added
title: ctrl+r in /model fetches today's model list, and aforge models --refresh does the same from a script
pr: 787
surface: [chat]
invalidates:
  - "The /model picker never fetches, so a model a provider shipped this morning waits a day for the catalog's 24-hour TTL (or for somebody to delete a cache file). It still never fetches on its own, but ctrl+r on the open picker asks the router for the newest list now: the picker keeps answering while it runs, the landed list is re-ranked under the typed filter, and the conversation gets `models · 612 · 9 new · a, b, c`."
  - "catalog.Options.Refresh existed and nothing in the chat called it. catalog.Refresh is Load with that option set and returns the fetch error Load drops; both v3 doors (local and --host) call it through a v3ModelShelf, an atomic pointer that /model's list, the vision gate and the task-model list read."
  - "The picker's placeholder was `filter · ↑↓ · → lanes · ctrl+t effort · enter · esc`, whole on a sixty-cell frame. Where the door offers a refresh it is `filter · ↑↓ · → lanes · ctrl+t effort · ctrl+r refresh · enter · esc`, and on sixty cells `enter · esc` are what give way; with no refresh behind the door it is the old line, still whole."
  - "The empty picker said `no model matches`. Where a refresh is offered it says `no model matches · ctrl+r fetches the newest list`."
  - "ctrl+r meant two things on the v3 surface (spell it out over a draft, reveal in /files). It means a third inside the /model picker, and only there — the settings panel's model rows, home's model list and the alt+o model list do not have it."
  - "`aforge models` parsed no flags and ignored anything after it except --help. It takes --refresh, and an unknown flag is now refused like on every other door with a flag set."
---

A failed fetch never changes the list on screen: the catalog already degrades to its
cache, the shelf keeps the catalog it had, and the only thing the failure adds is
`could not fetch the model list · <the transport's reason>` and the key offered again.
