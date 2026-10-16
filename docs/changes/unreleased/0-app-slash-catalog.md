---
kind: added
title: codeaf app reads the slash command catalogue from the engine
pr: 0
surface: [engine]
invalidates:
  - "The terminal's command table lived in `internal/tui3/commands.go` and nothing else could read it. It is `internal/slashcat`, and `tui3` adapts it; add or alias a command there, not in `tui3`."
  - "Sending `/cost` or any slash line through `agents.send` does not run the command; it is a message to the model, and `slash.catalog` lists words without running them."
---

`slash.catalog` (no params, read-only) answers every command the terminal chat
accepts: 48 commands over 82 rows with 28 aliases, folded by name into forms
and aliases, followed by the programs the running build carries (`senior-dev`,
`sec` and `review` on unix; none on Windows). Hello announces `slash.catalog`
only from an engine that serves it. Descriptions are the terminal's wording;
`host` is always `"unknown"` because `--host` is the terminal client's state.
At most 128 commands, 16 forms, 8 aliases per command and 512 bytes of text; a
program whose word is already a command is named under `skipped`.

The terminal's own list is byte-identical to before. Which actions have a typed
endpoint, and which are still terminal-only, is not part of this change; the
list is drawn from the engine, the actions are not yet.
