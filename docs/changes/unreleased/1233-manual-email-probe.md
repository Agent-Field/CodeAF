---
kind: fixed
title: asking can you access my email reaches connected accounts, not conversation search
pr: 1233
surface: [chat]
invalidates:
  - "Asking `can you access my email` retrieved J18 conversation-search sections about emailed receipt links (`what-i-remember`, `places`) instead of connected accounts. The accounts page now has a heading in those words, and the J18 headings still name emailed receipt links, purchase confirmation PDF, and original-passage ranking."
---

The search pages still say original authenticated-access passages rank from those
asks; they just no longer occupy every slot on a mailbox question.
