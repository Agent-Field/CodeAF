---
kind: fixed
title: a recursive delete outside the agent's folder asks, however its flags are spelled
pr: 0
surface: [engine, chat, app]
invalidates:
  - "Was true: the critical-command floor matched spellings, so `rm -rf /tmp/x` asked under a blanket allow but `rm -r /tmp/x`, `rm -R`, `rm --recursive`, `rm -r -f`, `find /tmp -delete` and `find /tmp | xargs rm -r` ran without a question; canvas agents deleted /tmp folders that way. Is true now: any recursive delete whose target is an absolute or home path, a variable, a `..` that climbs out, or a relative path after a `cd` away asks with `critical command \"<the command as written>\"`, and the \"stop asking me\" memo cannot swallow it. Relative deletes inside the folder the command starts in (`rm -r build`, `find . -delete`) stay ask-free."
---
