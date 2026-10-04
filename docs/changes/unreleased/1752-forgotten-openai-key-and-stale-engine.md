---
kind: fixed
title: forgotten OpenAI keys and stale folder environments no longer hide a connected key
pr: 1752
surface: [chat, engine, docs]
invalidates:
  - "The default OpenRouter key ladder was OPENROUTER_API_KEY, OPENAI_API_KEY of any shape, then the saved profile key. It is now OPENROUTER_API_KEY, the saved profile key, then OPENAI_API_KEY only when it starts with sk-or-. A custom CODEAF_BASE_URL keeps the old order and accepts any compatibility-key shape. An unusable OpenAI key is never copied into the profile."
  - "Unsetting a stale key and opening a new terminal was not enough: the folder engine kept the environment of the terminal that started it. A same-build idle engine now restarts immediately after a window disconnects to pick up the new terminal's provider and settings environment, while preserving journaled conversations."
  - "Picking up a changed shell environment always meant stopping the engine by hand. An idle engine now stands down automatically without force; a host with an attached window, streaming turn, waiting question or background work keeps it, and the window names a shell-quoted codeaf engine --stop --workspace <dir>. Status also reports a known environment difference."
  - "A running engine resolved new model sources with its boot-time default key. A newly connected profile key now reaches retained, reopened and joined conversations before their welcome, and later conversations on source refresh, while OPENROUTER_API_KEY still wins and a keyless reading keeps a working in-memory key."
  - "Doctor and the missing-key remedies recommended OPENAI_API_KEY without distinguishing its provider. Doctor now names the resolved rung and explains unused compatibility keys without revealing them; help, settings, the guide and the chat manual describe the new ladders and engine restart."
---

The engine compares only a local, private salted fingerprint. No credential or
provider address is added to the wire. Environment replacement disregards only
a disconnected window's watch grace; ended conversations hold no work. Ordinary
build takeover and the person's stop command keep their existing rules. Missing
or unreadable fingerprints keep the current host, and ordinary terminal bookkeeping never triggers a restart.
