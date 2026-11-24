---
kind: changed
title: A crew member's handle you type may be up to 24 characters
pr: 2891
surface: [engine, app, chat]
invalidates:
  - "A crew handle was 2 to 12 characters however it was made, so a typed name like review-and-testing was refused with \"a handle is 2 to 12 characters\". A handle a person or the manager gives may now be 2 to 24 characters (teams.HandleMax). Handles the engine picks itself (the word list's guess, the title model's choice, a model's crew proposal and clash numbering) still stay within 12 (teams.HandleWordMax)."
  - "members.start refused a bad handle with \"a handle is 2 to 12 lowercase letters or digits\"; it now says \"a handle is 2 to 24 lowercase letters, digits or hyphens\"."
---
