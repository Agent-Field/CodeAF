---
kind: added
title: desktop permission asks offer explicit saved scopes and automatic approval receipts
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "inbox.answer formerly dropped the chosen permission scope. It now accepts scope, rejects missing or foreign scopes with bad_scope before the hold, and saves through the same path as asks.answer for that item alone."
  - "Always formerly ignored batch_item and checked the parent ask's scopes. It now validates and releases the selected item alone. Automatic receipts and the Always receipt's settings link formerly disappeared on reload; both receipts now retain their placement and original fields through agents.entries."
  - "Automatic receipts formerly ended with the bare scope; they now say you always allow <scope>. Permission asks now carry outside for the longest same-program saved bash rule that does not cover the call; other tools and unrelated programs omit it. Terminal rendering is unchanged."
  - "The desktop always answer saved the full bash command and could fall back to a whole-tool session approval. It now requires an offered scope, saves exactly that rule, and keeps the question open if saving fails."
  - "The app wire had no permission scopes or reason for hiding always, and saved-rule allows left no receipt. Permission asks now carry scopes and always_ask; receipts name the saved scope, link to permissions settings, and explain subsequent matching allows."
---
