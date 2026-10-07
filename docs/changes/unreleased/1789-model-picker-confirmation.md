---
kind: changed
title: Choose a provider during setup and pick only available models
pr: 1789
surface: [chat, docs]
invalidates:
  - "Provider setup used to divide supported providers between a first page and More providers, with a selectable Skip for now row. It now has one scrollable list showing six providers at a time; Esc skips setup and no provider row skips it."
  - "Browser authorization URLs used to wrap across several visible rows. Codex, OpenRouter and the shared browser sign-in cards now show one short hyperlink retaining the complete URL. Setup adds Ctrl+Y to copy the complete sign-in URL; waiting cards retain their full-link copy action."
  - "A fresh profile used to ask for OpenRouter before offering any other model provider. Setup now begins with a choice of supported providers, retains the numbered connection and controls screens, and connects Ollama without asking for a key. Back cancels unfinished connections, and working connections bypass the chooser."
  - "Enter used to apply a model and leave the picker open until Esc. It now selects the model and closes the list immediately, returning to the conversation, task room, settings, Home draft or task composer."
  - "OpenRouter's public catalog used to appear even without an OpenRouter key. Every model picker now includes only provider connections with credentials or explicit anonymous access, including Ollama, and combines their own catalogs when several are connected."
  - "A cold picker used to offer five built-in guesses, and filtered or empty catalogs could fall through to older rows. Picker lists now use only the current provider's known catalog or its own cache while warming, with no built-in guesses."
  - "A local launch used to display its configured or shipped model even when the picker did not list it. Its default now comes from the available chat catalog; an absent choice is replaced by a listed model, and no known model leaves the draft held until discovery supplies one."
---

The draft is preserved when the model is chosen. A list with no matching model
stays open; Enter inside provider controls keeps their existing navigation.
