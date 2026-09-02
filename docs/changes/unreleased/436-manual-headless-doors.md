---
kind: changed
title: the chat manual can answer how to run aforge with nobody watching
pr: 436
surface: [chat, docs]
invalidates:
  - The chat manual described a headless run only from the far end — the last line it prints when it comes up short, and the crew it resolves its models through. It now has a section for aforge do that names every flag it takes today, says which of its output goes to standard output and which to the error stream, and gives the meaning of exit 0, 1 and 2.
  - No page in the chat manual mentioned aforge exec at all. There is now a section for it on the adaptive-runs page, saying it is one worker straight through with no plan behind it, and that --plan-model is accepted only so every headless door takes the same flags.
  - A figure a chat page quotes had to be a named constant to be pinned in truth_test.go. A flagNumber helper now reads a default out of the flag declaration that owns it, so exec's 200 turns and 150,000 tokens are answerable to the line the command's own help prints them from.
---

docs/HEADLESS.md has carried all of this for a long time and is not compiled into
the binary, so the chat could not read it. Asked "how do I run this from a
script", the manual reached a page about a run a conversation cannot start at
all, and the model improvised the rest.
