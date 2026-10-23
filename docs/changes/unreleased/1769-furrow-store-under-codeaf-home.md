---
kind: fixed
title: The furrow snapshot store follows CODEAF_HOME, not the login's data root
pr: 1769
surface: [chat, engine]
invalidates:
  - "furrow's snapshot store lived in the login's OS data root (`~/Library/Application Support/dev.furrow.furrow` on macOS), so two profiles that shared a HOME shared one store. It now lives under the state root (`CODEAF_HOME/furrow`, default `~/.codeaf/furrow`), a store found at the old root is moved there once on the next start, and a link left at the old root keeps an older binary resolving the same store."
  - "`CODEAF_HOME` was described as the whole state root but left furrow's store out; the `help env` row now names it."
---

On macOS and Linux, `codeaf chat` sessions launched with their own CODEAF_HOME
still wrote furrow's snapshot store under the shared HOME (and test runs on a
developer's machine wrote into the developer's own store). Every furrow child
now receives a `FURROW_DATA_DIR` under the state root, and the one previous
store a profile may already own is carried into its new home once, with a
link at the old root so an older binary reads the same store.