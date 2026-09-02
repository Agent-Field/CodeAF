---
kind: changed
title: web search settings land on the next search, and every search names its back end
pr: 263
surface: [chat, engine]
invalidates:
  - "The search pair was resolved once at boot, and the four search rows said a change lands on the next session. It resolves on every call now; a key or pin written in the sheet is used by the very next search."
  - "A search result never said which back end answered it. Its last line now reads `5 results · firecrawl`, the transcript row's stat slot says the same, and `/status` carries a `search` fact naming where the next search goes and whether a key is on it."
  - "The `searching` row cycled `auto → exa` on one press, silently pinning a back end with no key, and said nothing about it. The cycle is `auto → firecrawl → duckduckgo → exa → jina-search`, and a pin whose key is missing is spelled out on the row with the exact refusal every search will give."
---
