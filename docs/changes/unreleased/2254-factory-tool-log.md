---
kind: fixed
title: Factory tool logs show one tool name and portable paths
pr: 2254
surface: [engine]
invalidates:
  - "Tool log captions repeated the tool name and exposed absolute home paths. Tool captions now carry an optional kind and use portable paths."
---

Factory job logs show each tool name once. Tool captions replace home paths
with `~` and shorten the job checkout before limiting the visible text.
Recognizable saved tool captions receive the same treatment when read.
People's words stay as written; older readers can still display the text.
