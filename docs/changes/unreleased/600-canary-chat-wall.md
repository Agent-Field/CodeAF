---
kind: fixed
title: the canary chat door gets its own wall inside the rig clock, so chat rows measure an aforge ending
pr: 600
surface: [build]
invalidates:
  - "A chat row's `wall` verdict on any canary table up to 713945e3 read as aforge running out of time. It was the rig's tmux clock stopping at 900 s: `lib/chat.sh` passed `-max-cost` and no `-max-hours`, so every `--yolo` chat cell ran with a money ceiling and no wall, and no law keyed to the steward's own wall ever applied in a chat cell. The driver now derives `-max-hours` from its wall argument, 60 s inside the rig clock; do rows were always given `-timeout` and are unaffected."
---
