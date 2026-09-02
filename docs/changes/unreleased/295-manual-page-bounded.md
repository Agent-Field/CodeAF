---
kind: fixed
title: a manual page read is bounded like a file read, and the rest of the page is asked for by heading
pr: 295
surface: [chat, engine]
invalidates:
  - "The manual tool returned a named page WHOLE and unbounded, so a single lookup of the largest chat page could put 205 KB into the conversation. It is now cut at the read tool's own cap (bare.ResultByteCap, the exported form of internal/exec/bare's defaultMaxBytes), and the cut says how much of how much it is."
  - "The manual tool took `query` and `page`. It now also takes `section`: a page over the cap comes back with the list of its `## ` headings, and asking for the same page with one of those headings in `section` returns that section whole. An unknown heading is refused by name."
  - "The manual tool description no longer says \"what a command or key does\" — that phrase was a word-for-word copy of prompts/system.md's Tool Policy line for this tool, and it paid for the third schema argument. The fixed prefix went 47,745 to 47,843 bytes of its 48,000 budget."
---

A page lookup was the one read on the belt with no ceiling, and the pages it can
return are larger than the files: ten chat pages are over 50KB and the biggest is
four times what `read` will hand over. The refusals made the call likelier, since
they hand the model the list of page names and a name is what a page lookup takes.
Bounding it alone would have made most of the manual unreachable, so the bound
carries its own way out — every heading of the page it cut, each of them an
address the next call can use.
