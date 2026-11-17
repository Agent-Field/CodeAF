---
kind: fixed
title: A client tool set may carry 8192 instruction bytes, and a refusal names the size it saw
surface: [engine, app]
invalidates:
  - "agents.tools_set allowed 4096 instruction bytes per set. It now allows 8192 (session.ClientToolInstructionsMax). The desk's workstream set carries WorkstreamWorkRules (about 3 KiB) plus its own lines; its Afi set came to 4521 bytes, was refused, and Afi in a Personal workstream had no ws_* tools at all."
  - "The refusal read \"a set allows at most 32 tools and 4096 instruction bytes\" whichever limit was crossed. It now names the one limit and the size sent: \"a set allows at most 8192 instruction bytes; this one has N\" or \"a set allows at most 32 tools; this one has N\"."
---
WorkstreamWorkRules may take at most half of a set's instruction bytes; a test
holds it there so the rules cannot crowd out the lines an app adds.
