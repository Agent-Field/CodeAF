---
kind: added
title: custom connections are named, sit beside each other, and switch from the Providers tab
pr: 1089
surface: [chat, engine]
invalidates:
  - "A custom connection was one unnamed row: codeaf derived its name from the host and that name was final. `/connect` asks for a name after the base URL now, with the host's own spelling pre-filled, so a connection to `127.0.0.1` is called `127-0-0-1` unless you type another. A name carrying `/` or a space is refused in the box, with the reason, and a name another service or a default-service model author already uses is settled in the same attempt: codeaf takes an available spelling and the connect line names what it used."
  - "A second custom connection replaced the first, because both were stored under one id. Custom connections now coexist as named instances, each with its own row, its own key, its own cache and its own heading in `/model`."
  - "Connecting, editing or switching a custom connection meant opening `/connect`. The Providers tab in `/settings` now ends its services section with an `add custom connection` row and, once a custom connection is connected, an `active connection` row: enter on the add row connects a new one, enter on a connected service row edits it, and enter on the active connection row moves this conversation onto the next one, wrapping around."
  - "Renaming a connection was not possible; its name was whatever the host slug produced, for good. Enter on a connection's Providers row edits it now, and a changed name is a rename that follows the picks: the conversation's own model id is re-spelled under the new name (a turn still answering is waited out first) and every stored id moves with it, reasoning levels, role pins, the fallback chain and the capability slots. The model itself does not change; a rename is a change of label, not of mind."
---

A connection's name is its routing prefix: the first segment of every model id the
connection qualifies (`homelab/glm-5.3`), the heading its models sit under in
`/model`, and the row's name in `/connect` and on the Providers tab. The active
connection is read from the model the conversation is on and stored nowhere, so the
switcher rewrites the pick through the same write the `/model` picker makes and there
is no second source of truth to disagree with what is answering.
