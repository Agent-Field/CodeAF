---
kind: added
title: Workstreams start from a terminal, and an organization's agents can read the web
pr: 2888
surface: [engine, factory, app]
invalidates:
  - "An organization workstream's agent on the server had no web tools and its prompt said it cannot use the web. It now has web_search and web_fetch (read only, through the engine's search and fetch providers) and is told so."
  - "The codeaf command line had no way to start or follow workstream work. codeaf org login, codeaf task and codeaf task follow now start a task in an organization's workstream, follow it on one line, and answer a quick Ask in the terminal."
  - "A long Claude Code turn could read as an over-full window and wait twenty minutes for a summary. It no longer counts as over-full, and the summary waits two minutes."
  - "Answers that need the person's own words could be given in one click, and an answered run did not say who answered. Such answers now always ask for words, and the run says who answered."
---

The terminal door keeps its sign-in in the codeaf home under doors/, readable by the person only, and a decision Ask is
never answered there: the door says to open WorkAF.

Review fixes: the server agent's web_fetch reads only an address a person wrote (their chat messages and steers, a
task's title and request when a person last wrote them, a typed answer) or one from that run's web_search results, and
never sends a fragment on; an answer kept while the organization had no model key is carried on once a key is set, or
at start when one is there. A plain button answer on an Ask that also offers a words answer can still be undone. The terminal
door refuses a password over plain http to another machine, passes a server's 403 sentence through, never replaces an
unreadable sign-ins file, locks its sign-ins while writing them, strips control characters from what the server sends,
asks a quick Ask again after a failed answer (not a refused one) or an undo, and lets the workstream's fallback answer as the server allows.
