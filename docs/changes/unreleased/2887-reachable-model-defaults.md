---
kind: fixed
title: Default models come from a connected provider when OpenRouter is not connected
pr: 2887
surface: [engine, app, chat]
invalidates:
  - "The built-in defaults (the conversation model, the fallback models row, the reflex and small work helper tiers, the free helper and chat defaults, and the knowledge model) were OpenRouter ids used whatever was connected, so a ChatGPT-only profile failed every message with no API key and an Ollama-only profile still sent its fallback, titles and routing to OpenRouter. Each built-in default is now kept only while a connected service can send it; otherwise every slot follows the conversation model's provider: its preferred or strongest model for the conversation, a different strong model for the fallback (none when it offers one model), and its lightest model for the helper tiers and knowledge. With nothing connected the built-in ids stay named as before."
  - "ChatGPT's crew routes served only the vendored preferred model (gpt-5.5), which a ChatGPT account may not list; they now serve the account's remembered models, and the preferred id only while nothing is remembered."
  - "ChatGPT's remembered account models now keep their thinking levels, used only to rank them; the rows still do not claim a published reasoning profile."
---
