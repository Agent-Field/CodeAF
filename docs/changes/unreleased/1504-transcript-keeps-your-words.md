---
kind: fixed
title: a message that carries skills shows your words, not the skills' description sheet
pr: 1504
surface: [chat]
invalidates:
  - "A message whose words matched skills on the shelf printed a `Skills suited to this message:` block as part of the person's own message in every transcript: the block was chosen for the model and said nothing the person typed. The block still reaches the model — the turn reasons from it exactly as before — and the transcript keeps the person's words; the dim `skills carried` line under the message is the one thing a surface draws about what the turn carried."
---

The skills a turn carries are chosen from that message's words, so they belong
to the turn and not to the conversation's record. The person's own row keeps
its own words: what the model reads grows by the block, what every surface
replays is what was typed, and the carried skills are named once, in the dim
line the transcript already had.
