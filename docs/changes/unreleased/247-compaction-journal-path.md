---
kind: changed
title: a fold marker names the journal path the model can grep, not a store id
pr: 247
surface: [engine, chat]
invalidates:
  - "A cross-turn fold stood in as `[folded 31 messages · store:104..store:189]` or `[folded 31 messages · journal:line-12]`. Neither is a path `grep` or `read` can open. The marker now names the session journal's real filesystem path and the tools that open it: `[folded 31 messages · grep or read /home/x/.aforge/v3/sessions/abc.jsonl, lines 12..40]`."
---

The original lines stay above the compaction marker in that JSONL, so the path
is the recovery floor and `store:` is no longer the pointer. The path is its own
word with the lines said after it, because `path:12..40` is a token neither tool
takes. A session with no journal file still names no path — the store where
there is one, the weaker true thing where there is not — which is the emptiness
law rather than a file that does not open. The system prompt is unchanged: the
affordance rides on the marker, which only a conversation that folded pays for,
instead of on the fixed prefix every request carries.
