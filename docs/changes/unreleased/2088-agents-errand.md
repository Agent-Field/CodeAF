---
kind: added
title: The app can ask an agent for one short metered model call with its own prompt
pr: 2088
surface: [engine, remote]
invalidates:
  - "Was true: the only app-wire model errand was agents.suggest_reply, whose system prompt and one-line output were codeaf's own and fixed; nothing on the app wire took a caller's system text. Is true now: agents.errand runs one detached, tool-less completion with the caller's system text and messages on a held agent's metered door, by role or explicit model inside the agent's model policy, as text or one JSON object, and answers the model that answered, its provider, role, usage and time, or an empty text with a machine reason (closed, stopped, over_budget, busy, deadline, cancelled, model_failed, invalid_json, not_open, unavailable)."
  - "Was true: the app had no way to learn which roles and models an errand could use. Is true now: agents.errand.models lists each role's current model on the agent (or profile) and every model this engine can serve, with JSON support where a catalog knows, and the hello carries an errand caps object (v1) with the limits."
  - "Was true: agents.suggest_reply had its own admission, cache and one-in-flight slot, cached failed requests, and passed no floor model to the naming role. Is true now: it runs on the errand core with purpose reply_suggestion, the conversation's model as the naming role's floor, and a cache that keeps every answer the model gave but asks a failed request again."
---

The engine never branches on an errand's purpose: it is the ledger label of the
receipt and the key of the one-in-flight slot.
