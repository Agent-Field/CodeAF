---
kind: fixed
title: The tray pager's end arrow is 40% opacity
surface: [desktop, chat]
invalidates:
  - "The tray pager's disabled arrow used the shared disabled-control opacity of 0.6. It is 40%, on both the question pager and One by one."
---

Shared disabled controls stay at 0.6. Only the pager's end arrow uses the tray token.
