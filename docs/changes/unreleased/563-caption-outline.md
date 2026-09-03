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
  - "A finished step of one tool call had no caption, so most turns still looked like a raw tool pile. Every step gets a heading now, including a singleton."
  - "Sequential tool rounds on a live turn were drawn as one open cluster, so a finished caption could not sit above a live one. Each finished round collapses to its caption; only the live frontier keeps its rows."
  - "Opening the worked chip drew caption lines whose first click did nothing useful, and a shut past caption could swallow the live frontier. The outline and the click now share one open/shut rule, and each caption advances only over its own step."
  - "Live captions fell back to `running N calls` when the model only thought and never wrote a line. The floor now names the work (`listing github issues`, a short bash gloss); the cheap narrator arms after half a second instead of four."
  - "A caption could be the first line of the model's thinking. Thinking stays machinery under the step; the caption is only a narrating line, a narrator step title, or a tool-floor gloss — the discrete checklist item, never the chain of thought."
---
