---
kind: changed
title: caption sentences now head work, and ctrl+e opens their outline before machinery
pr: 563
surface: [chat, engine]
invalidates:
  - "The fold head above a long tool cluster was `N earlier tool calls · ctrl+o`. It is now a one-line caption of what that step is trying to find out, with the call count on the right; ctrl+o or a click still opens the rows."
  - "`ctrl+e` on an empty box opened a `▸ worked` chip onto the tool rows. It now opens the chip onto the outline of captions; expanding a caption shows the machinery."
  - "The system prompt told the model not to narrate obvious steps with no exception. Planning now also asks for one short present-tense line before a batch of tool calls."
  - "Live work on the conversation and on a task page was a pile of tool rows that collapsed into a counted chip. Both surfaces now share captions: past steps collapse to the sentence while the turn is running, and the chip still hides machinery after the answer."
---
