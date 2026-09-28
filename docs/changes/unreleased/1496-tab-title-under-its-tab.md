---
kind: changed
title: A hovered tab's full title sits on the blank row and ends under its tab
pr: 1496
surface: [chat, docs]
invalidates:
  - Hovering a tab drew its full title from the left margin on the row right under the tab strip, over the rule; it now sits on the head's blank row below the rule, right-aligned to the hovered tab's right edge (close mark included), sticking out to the left when wider than the tab and reaching right only when it cannot fit left of that edge.
---
