---
kind: fixed
title: Tab selects the highlighted slash command on Home and in conversations
pr: 1642
surface: [chat, docs]
invalidates:
  - "Tab previously ignored the slash command list in a conversation and navigated away from Home. It now selects the highlighted command in both composers, including the new-chat screen: bare commands run, argument rows complete their prefix, and words inside a sentence complete without submitting it."
  - "Tab with an unmatched slash command on Home previously switched pages. It now leaves the draft and screen unchanged, while Enter retains its existing submission behavior."
---
