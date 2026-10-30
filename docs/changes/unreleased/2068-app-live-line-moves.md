---
kind: fixed
title: codeaf app says an agent's live line again as its turn moves to each call
pr: 0
surface: [engine]
invalidates:
  - "An agent's `line` changed when its running turn began a new call, but no `agent.changed` said it, so a window kept the line of whatever road last said the agent (often none) for the whole turn. Each new call is now said as the agent's `line`, at most once a second per agent, and the newest line is always said."
---

The line is the title of the call the turn is on, the same title its
`turn.tool` row carries: only what the agent did, never a guess at its intent.
A burst of quick calls says the first at once and the last when the second has
passed. The turn's end says the agent again with no line, and a line still
waiting at that moment is dropped.
