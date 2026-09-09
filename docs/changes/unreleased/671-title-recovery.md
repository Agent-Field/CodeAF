---
kind: fixed
title: Unusable naming replies recover and compact tabs reveal their full names on hover
pr: 671
surface: [chat, engine, docs]
invalidates:
  - "An empty or invalid successful naming response ended the conversation's naming errand. It now tries the existing fallback ladder and bounded retries; task naming also refuses placeholder replies before falling through."
  - "Markdown around Full and Tab labels could become part of a saved name. Parsing now removes that formatting before reading the pair, and saved Full prefixes are cleaned when read."
  - "Tab labels were bounded only by characters and could contain a sentence. Generated and reopened tab labels now have at most two words; hovering shows the full name in a stationary wrapped preview."
---

The rental conversation recorded a successful one-token naming reply and stayed unnamed.
Its second task was saved as `nothing to name`; a second conversation saved `Full:` as
part of both names. Naming recovery remains separate from foreground work and preserves
usage for every answered call, including rejected replies. Hover changes no tab geometry.
