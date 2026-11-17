---
kind: changed
title: Claude Code lists versions of each model, and a picked version is the one that runs
pr: 2611
surface: [engine, app, chat]
invalidates:
  - "The Claude Code service listed three models: opus, sonnet and haiku. It now lists nine: those three words, then the two newest versions of each family under Claude Code's own names (claude-opus-5-5, claude-opus-5, claude-sonnet-5-5, claude-sonnet-5, claude-haiku-5-5, claude-haiku-4-5)."
  - "Every Claude model id sent to the Claude Code service was read down to its family word, so claude-code/claude-haiku-5-5 ran whatever the installed Claude Code maps haiku to. A name in Claude Code's own spelling is now sent whole, for a conversation, a toolless call and a crew seat's --claude-model; another catalog's spelling such as anthropic/claude-opus-4.6 is still read as its family."
  - "codeaf connect claude-code ended with 'models: claude-code/opus, claude-code/sonnet, claude-code/haiku'. It now ends with 'models: claude-code/opus, claude-code/sonnet, claude-code/haiku and 6 versions'."
---

A family word runs whichever version the installed Claude Code maps it to. On an
install older than a release, the Haiku row was the previous Haiku and nothing
said so; the version rows let a seat be held to one version whatever the word
means on that computer. The version list is a table in `internal/claudemodel`.
