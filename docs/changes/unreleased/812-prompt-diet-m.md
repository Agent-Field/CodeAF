---
kind: fixed
title: The lean prompt profile keys off the context window and an explicit pin, never off the worker seat
pr: 812
surface: [chat, engine]
invalidates:
  - "The lean prompt profile had three triggers — the pin, the crew's `worker` seat, and a window under 32,000 tokens. It now has two: the pin `AFORGE_PROMPT_PROFILE=lean`, and a context window under 32,000 tokens. `Config.atTheWorkerSeat` and the `THE SEAT BEFORE THE WINDOW` branch in `resolvePromptProfile` are gone."
  - "An open-weight model was treated as a small model. It is not: `deepseek/deepseek-v4-flash-0731` and `z-ai/glm-5.3-flash` — the models the frugal and balanced crew presets put in the `worker` seat — are served with 128,000 tokens of room, so a conversation on one of them now renders the FULL page, byte-identically to any other 128,000-token model, with the full tool list and saved memories on. A licence is not a size."
  - "`internal/manual/chat/models-and-cost.md` said lean applied \"under 32,000 tokens, or on that seat\". It now says lean applies in exactly two cases, and it carries a second section, `## Is an open-weight or local model given the lean profile?`, saying that deepseek and glm with a large window are not lean."
  - "There is still no `/settings` row for the profile. The environment pin is the only way to choose it by hand."
---

The seat trigger was silent, and that is what made it wrong. A person on the
frugal preset who then picked their worker model in chat would have had sections
taken off the page, `propose_task`, `tasks`, `watch`, `track`, `commit`, `recall`
and `read_document` shelved, and saved memories turned off — with nothing on
screen saying so, because a derived profile has no row anywhere to read. A model
that is genuinely small announces it: llama.cpp, ollama and LM Studio all report
the window the loaded model was given, and that is the fact the profile reads.
Where an endpoint reports a window its model does not really have, the pin is the
way in.
