---
kind: fixed
title: a critical-command question names the command it caught, not the table pattern
pr: 2824
surface: [engine, chat, app]
invalidates:
  - "Was true: a critical shell command's question and refusal quoted the floor's table pattern, so `rm -rf /tmp/site_check` was asked about as `critical command \"rm -rf /*\"` and a `!mkfs.x` as `critical command \"mkfs*\"`. Is true now: the rule quotes the segment of the line that matched, as written with whitespace collapsed and cut at 120 characters (`critical command \"rm -rf /tmp/site_check\"`); the fork bomb still reads `:(){:|:&};:`. Which commands ask is unchanged."
---
