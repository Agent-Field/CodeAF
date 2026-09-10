---
kind: changed
title: tasks can search and read earlier conversations without filesystem discovery
pr: 769
surface: [chat, engine, docs]
invalidates:
  - "A task worker, nested worker, forked hand and task checker had no search_conversations tool because they received no writable memory store. They now inherit a read-only conversation-history interface and the tool, without enabling memory extraction, memory writes or worker-message indexing."
  - "search_conversations only accepted query and limit, returned the start of matching messages, and required a sibling transcript path for further reading. It now supports conversation-scoped search, recent-message browsing and opaque source-reference reads with surrounding messages; explicit reads preserve the full indexed anchor and its line breaks."
  - "Broad conversation searches could rank the asking conversation's own fresh question and tool calls first. They now exclude only the asking agent's own thread; an explicit session_id includes it, and workers can still search their parent's history."
  - "A task-search miss only suggested more task searches. It now points to conversation search when that tool is available and no matching work was found in the requested scope."
  - "A history query error was indistinguishable from a miss, and short or non-ASCII query words were dropped by the model-facing search path. The new reader reports database and cancellation errors and accepts safely quoted Unicode words, while retaining lexical BM25 ranking."
---

The same tool searches across places, searches inside a known conversation, and
opens a source message from a reference copied verbatim. Results carry conversation and message IDs, timestamps,
stored speakers, bounded neighbouring context and history boundaries. A full
indexed message is at most 16 KiB; oversized spilled-file contents and unindexed
history remain outside the index. Search remains lexical, not semantic.
