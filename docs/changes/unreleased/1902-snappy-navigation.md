---
kind: fixed
title: show first messages immediately and refresh Home behind the screen
pr: 1902
surface: [chat, docs]
invalidates:
  - "Sending from Home or the new-chat page left the starter visible until session creation finished. A first-message screen now acknowledges the send while opening runs in the background, before any model token arrives."
  - "Every Home visit and beat synchronously read conversation, memory, spending and folder data. Home now paints its last completed reading and refreshes those inputs off the UI loop, coalescing reads across visits."
---

Page-entry spending refreshes also run beside navigation. Failed or cancelled
opens preserve drafts and attachments. Real-terminal timing coverage separates
navigation, message acknowledgement, an 800 ms backend stall and live-model
answer visibility; measurements and model receipts stay outside the repository.
