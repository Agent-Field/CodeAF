---
kind: changed
title: Choose a provider during setup and pick only available models
pr: 1789
surface: [chat, docs]
invalidates:
  - "The later add-provider and /connect menus used to omit OpenRouter and treated every saved provider row as connected. Both now include all nine initial setup options and read actual credential availability; the add menu labels connected and not connected rows. OpenRouter can be connected after Ollama through its existing browser/key form without repeating onboarding, losing the draft or removing Ollama. Its profile key can also be disconnected with two-Enter confirmation and is revoked in the running session; shell keys remain connected until unset and restarted."
  - "Provider selection used to start on Ollama. OpenRouter is now first and initially selected, with Ollama second; the other seven options keep their order."
  - "Choosing Codex during setup used to start browser sign-in immediately. Codex now has the same enter connects in browser confirmation as OpenRouter; selecting the provider opens its connection screen and Enter starts sign-in."
  - "Provider setup used to divide supported providers between a first page and More providers, with a selectable Skip for now row. It now has one flat list showing all nine providers, with no scrolling viewport or page counter; Esc skips setup and no provider row skips it."
  - "Browser authorization URLs used to wrap across several visible rows. Codex, OpenRouter and the shared browser sign-in cards now show one short hyperlink retaining the complete URL. Setup adds Ctrl+Y to copy the complete sign-in URL; waiting cards retain their full-link copy action."
  - "A fresh profile used to ask for OpenRouter before offering any other model provider. Setup now begins with a choice of supported providers, retains the numbered connection and controls screens, and connects Ollama without asking for a key. Back cancels unfinished connections, and working connections bypass the chooser."
  - "Enter used to apply a model and leave the picker open until Esc. It now selects the model and closes the list immediately, returning to the conversation, task room, settings, Home draft or task composer."
  - "OpenRouter's public catalog used to appear even without an OpenRouter key. Every model picker now includes only provider connections with credentials or explicit anonymous access, including Ollama, and combines their own catalogs when several are connected."
  - "A cold picker used to offer five built-in guesses, and filtered or empty catalogs could fall through to older rows. Picker lists now use only the current provider's known catalog or its own cache while warming, with no built-in guesses."
  - "A local launch used to display its configured or shipped model even when the picker did not list it. Its default now comes from the available chat catalog; an absent choice is replaced by a listed model, and no known model leaves the draft held until discovery supplies one."
---

The draft is preserved when the model is chosen. A list with no matching model
stays open; Enter inside provider controls keeps their existing navigation.
