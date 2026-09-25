---
kind: changed
title: tui3 head grows an air row under the top navigation line
pr: 1509
surface: [chat]
invalidates:
  - "The tui3 page head was four rows under the top navigation line. It is five: a blank air row sits directly under the nav line, then the tab strip, divider, and margin row."
---

Every page's head pays one extra row for the air row; the welcome screen's
budget and the tui3 tests were adjusted with it. Visual proof lives at
`docs/design/spark-header-spacing.png` (live Spark tmux capture).
