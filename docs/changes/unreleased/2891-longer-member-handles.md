---
kind: changed
title: A crew member's handle you type may be up to 24 characters
pr: 2891
surface: [engine, app, chat]
invalidates:
  - "A crew handle was 2 to 12 characters however it was made, so a typed name like review-and-testing was refused with \"a handle is 2 to 12 characters\". A handle a person or the manager gives may now be 2 to 24 characters (teams.HandleMax). Handles the engine picks itself (the word list's guess, the title model's choice, a model's crew proposal and clash numbering) still stay within 12 (teams.HandleWordMax)."
  - "members.start refused every bad handle with \"a handle is 2 to 12 lowercase letters or digits\"; it now gives the store's own reason (\"a handle is 2 to 24 characters\", \"a handle is lowercase letters, digits and hyphens\", or \"everyone is reserved\")."
  - "team_start's schema told the manager \"2 to 12 lowercase letters or digits\"; it now names the store's range (2 to 24, letters, digits and hyphens) from the same constants."
  - "The older one-object crew proposal format kept any valid model handle; its handles are now shortened to 12 like the streamed format's."
  - "An older codeaf (before this change) still accepts only 2 to 12, so a team synced to or loaded by it replaces a longer typed handle with a word-list guess."
---
