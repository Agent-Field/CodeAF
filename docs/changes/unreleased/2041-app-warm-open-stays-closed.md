---
kind: fixed
title: codeaf app keeps a closed agent closed when a window only warms it
pr: 2041
surface: [engine]
invalidates:
  - "Was true: every agents.open took an agent out of Closed or the archive, so a window's prefetch (a hover over its row, the × under the pointer) could reopen an agent the person had just closed, and its row came back. Is true now: agents.open takes warm (omitempty; an older engine ignores it). A warm open of an agent marked closed or archived is refused with conflict and the mark stays; an open agent is opened as before. An open without warm (the person's own, Reopen) still takes it out."
  - "Was true: a close let the conversation go and then wrote its mark after releasing the agent's gate, so an open could land between the two and leave the agent marked closed but held. Is true now: the release and the mark run under the agent's gate; an open that waited on it finds the mark."
---
