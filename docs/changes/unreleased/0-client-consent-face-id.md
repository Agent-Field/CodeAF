---
kind: fixed
title: Require Face ID for every consent a queued answer resolves
surface: [engine, app, chat]
invalidates:
  - "Checking only the named reversible ask could approve an irreversible batch sibling. Queued answers now check the whole resolved set and keep a refusal in the chat."
---

The same rule covers an irreversible target selected with batch_item.

Authorization runs against the fixed target set in the real answer path, so an
irreversible sibling arriving during the claim round trip is covered as well.
