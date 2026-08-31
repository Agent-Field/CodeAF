---
kind: changed
title: the forming block shows the brief being written, and several tasks forming share one block
pr: 125
surface: [chat, engine]
invalidates:
  - "`shaping the brief…` showed only a spinner and a clock. One dim row under it now carries the newest line of the brief as the shaper writes it, and it is empty until there are words to show."
  - "There was no way to read the brief before it landed. `→` with an empty box, or a click on the preview row, opens the last six lines of it; `←` or another click shuts it. No new key was added — it is the fold every block already has."
  - "Two `/task` commands shaping at once drew two blocks stacked at the transcript tail. They now share one block, `tasks · 3 forming`, with one compact row each and the preview under the pointed row only. `↑`/`↓` walk those rows."
  - "The shaper's call could not be observed at all. `session.WithBriefWatch` hands the accumulated answer to the one caller that asked for it; the conversation's own stream observer is still shut out, so nothing is typed into the room."
  - "A task approved from a proposal card draws no preview row. That brief was written before the person was asked, so there is no stream behind the wait."
---

Thirteen seconds of a number going up, in front of somebody who had just typed a
command and had no way to tell a careful model from a stuck one. The words were
being written the whole time; they simply had nowhere to go. The preview is a
TAIL and never a feed — one row whatever arrives — so nothing above it moves and
no row on the screen jumps.
