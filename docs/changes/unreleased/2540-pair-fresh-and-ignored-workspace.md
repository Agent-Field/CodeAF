---
kind: fixed
title: Empty chats can pair, and chats in Git-ignored project folders can save and move
pr: 2540
surface: [chat, engine, remote, docs]
invalidates:
  - "Opening and quitting an empty chat could prevent pairing without --replace; startup inventory and an empty owned working folder now allow code and link pairing while submitted work keeps its refusal."
  - "A project inside a Git-ignored parent could fail to save or lose the conversation; Git's answer now excludes the project files while the chat's own words still save and move."
---
Both pairing roads accept a computer that only opened codeaf. A submitted chat
still produces the counted refusal, and saved memories and settings retain their
existing checks.

When Git ignores the whole project, its entries stay out of saved history and
the separately saved conversation can move. Nested repositories and Git
discovery ceilings keep Git's own meaning. A genuine Git failure still stops
saving instead of admitting private files.
