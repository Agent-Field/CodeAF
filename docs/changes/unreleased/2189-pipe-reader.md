---
kind: fixed
title: A piped command whose reader stops early no longer reports failure
pr: 2189
surface: [chat, engine, app]
invalidates:
  - "Since every bash door ran with pipefail, a piped command whose reader stopped early (for example `git log | head`) reported failure with status 141. Now such a command succeeds when its last stage succeeded and every earlier failing stage ended with 141; any other failing stage still fails the command."
---

A piped command whose reader stops early (for example `git log | head`) no
longer reports failure. A pipeline whose 141 decided an `&&` or `||` list, a
command under `set -e`, and a command with its own EXIT or ERR trap keep the
status bash gives them.
