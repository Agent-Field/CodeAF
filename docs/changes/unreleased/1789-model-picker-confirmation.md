---
kind: changed
title: Model pickers list usable providers and close when a model is chosen
pr: 1789
surface: [chat, docs]
invalidates:
  - "Enter used to apply a model and leave the picker open until Esc. It now selects the model and closes the list immediately, returning to the conversation, task room, settings, Home draft or task composer."
  - "OpenRouter's public catalog used to appear even without an OpenRouter key. Every model picker now includes only provider connections with credentials or explicit anonymous access, including Ollama, and combines their own catalogs when several are connected."
  - "A cold picker used to offer five built-in guesses, and filtered or empty catalogs could fall through to older rows. Picker lists now use only the current provider's known catalog or its own cache while warming, with no built-in guesses."
---

The draft is preserved when the model is chosen. A list with no matching model
stays open; Enter inside provider controls keeps their existing navigation.
