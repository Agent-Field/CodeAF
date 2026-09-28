---
kind: changed
title: one message carries the skills its own words choose, and a person's attachments always ride
pr: 1345
surface: [chat]
invalidates:
  - "The chat chose the model's skills by scoring the shelf against the workspace path alone, so the same window of skills was offered on every turn and a skill named in the message could be dropped. The choice is now made from the text of each message, rendered with the turn rather than in the system prompt, and a hand-attached skill is never scored away or windowed."
  - "The skill catalog's sentence said skills are prepended to task work and used through task nodes. The catalog says what it is — the shelf this project holds — and says skills suited to a message are attached to it, with `use_skill` reaching any of them by name."
---

The catalog stays the menu: a windowed, stable section of the prompt prefix.
What changed is the choosing half, which was missing. A turn's block is composed
through the plan road's own PinnedSkills/RetrieveSkills/ComposeSkills, capped
at four with the person's attachments exempt from the cap, and the turn reports
the names it carried as one notice. The system prompt is a cached prefix, so the
per-turn choice rides the message the model reads; the journal keeps the words
the person typed.
