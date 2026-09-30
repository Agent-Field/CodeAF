---
kind: internal
title: the tmux suite reads providers in /connect and opens folded landings and skills
pr: 1680
surface: [build, docs]
invalidates:
  - "TestTUIE2E waited for `models` in the /connect panel, pressed a landing card's key repeatedly waiting for its receipt on the card, and waited for a carried-skill line on the turn itself; four subtests were red on dev 461a43fb6 while the product was right. It now reads `providers`, presses once and waits for the receipt in the conversation, and opens the `▸ worked` disclosure to find the skill line."
  - "skills-a-turn-used.md said the `skills · …` line stays under your message and is not folded with the turn's steps. Since #1627 it folds into the `▸ worked` line when the answer lands; the page now says so and names ctrl+e to open it."
---
