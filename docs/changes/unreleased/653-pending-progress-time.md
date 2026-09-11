---
kind: fixed
title: Keep elapsed feedback beside a completed step while the turn continues
pr: 653
surface: [chat]
invalidates:
  - "After a tool batch finished, compact progress showed only an animated dot unless the next model request was still waiting for its first response. A turn could continue reasoning for minutes while its latest useful caption carried no elapsed feedback. After ten seconds, the same inline place now says `still working · <elapsed>` from the conversation's turn clock; a known first-response wait keeps the more specific `awaiting response · <elapsed>`, and a lost connection remains immediate."
---

The completed caption remains static and is never described as still running. The
elapsed phrase belongs to the conversation continuing after it, uses only spare cells,
and falls back to the animated dot on a narrow row. Active captions and tool calls keep
their own step clocks. Task rooms do not borrow the main conversation's turn or request
clock.
