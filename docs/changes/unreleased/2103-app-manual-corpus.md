---
kind: added
title: codeaf app can register the desktop app's own manual pages
pr: 0
surface: [engine, chat]
invalidates:
  - "Was true: the chat's `manual` tool read only codeaf's own pages, so an assistant in the desktop app could not look up how the app itself works. Is true now: `manual.corpus.set {pages: [{name, markdown}]}` (hello says `manual.corpus.set`) replaces the app's pages, in memory for the engine's lifetime, at most 64 pages of 32 KiB and 512 KiB in all, each name a lowercase slug. The `manual` tool searches and reads them beside codeaf's pages, filed as `workaf/<name>`; codeaf's own manual corpus and its truth tests are unchanged."
---
