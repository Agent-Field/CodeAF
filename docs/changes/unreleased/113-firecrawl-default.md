---
kind: changed
title: web search is Firecrawl by default, keyless, on both surfaces
pr: 113
surface: [chat, engine]
invalidates:
  - "The chat's zero-key search default was DuckDuckGo's HTML endpoint. It is now Firecrawl's keyless MCP endpoint; DuckDuckGo remains as a pin."
  - "The swepro `websearch` tool was off the belt unless a CODEAF_ENABLE_* flag was exported, and split sessions 50/50 between Exa and Parallel. It is always on the belt and defaults to Firecrawl; the session-hash split is gone (EMBEDDING.md D13)."
  - "`CODEAF_WEBSEARCH_PROVIDER` accepted only exact lowercase `exa`/`parallel`. It is now trimmed, case-insensitive, and also accepts `firecrawl`."
  - "There was no Firecrawl key row. `search.firecrawlKey` (`FIRECRAWL_API_KEY`) exists, guarded, optional; `jina-search` became pinnable alongside it."
  - "Every manual edit committed the same generated `internal/manual/*.pack.gz` binaries, so otherwise-independent PRs conflicted after each merge. Manual archives are now ignored build products used by `make build`; ordinary Go commands embed the Markdown directly from a clean checkout."
---
