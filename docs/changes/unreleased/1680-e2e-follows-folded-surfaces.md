---
kind: internal
title: the tmux suite reads providers in /connect and opens folded landings and skills
pr: 1680
surface: [build]
invalidates:
  - "TestTUIE2E waited for `models` in the /connect panel, pressed a landing card's key repeatedly waiting for its receipt on the card, and waited for a carried-skill line on the turn itself; four subtests were red on dev 461a43fb6 while the product was right. It now reads `providers`, presses once and waits for the receipt in the conversation, and opens the `▸ worked` disclosure to find the skill line."
---
