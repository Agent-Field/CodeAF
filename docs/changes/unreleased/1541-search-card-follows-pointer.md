---
kind: fixed
title: Home search's card follows the pointer and returns to the cursor's match
pr: 1541
surface: [chat, docs]
invalidates:
  - "Since #1071, pointing at a Home search match could draw no card, and leaving the list left the card on the hovered match. A hover moved the keyboard cursor, and the card read only that cursor. While a search is typed, the pointer now previews the match under it without moving the cursor, and moving off the matches gives the card back to the cursor's match. Resting home keeps one shared selection."
  - "A `folder gone` search match has a card; its unavailable folder actions say so on it."
---
