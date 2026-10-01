---
kind: fixed
title: the model picker keeps the cursor's row on screen at every width
pr: 1696
surface: [chat]
invalidates:
  - "Service and machines' headings and reason lines could push the cursor's row past the drawn edge, including the opening frame. The window now counts screen lines, so /model and every model door open visibly on the model they hold; enter immediately confirms, and a refresh returns to that held model with the filter kept."
  - "At the bottom of the model list the cursor could disappear because the scroll window counted rows while the frame spent extra lines on headings and phone tails. Repeated arrows, page keys and wheel notches now keep the cursor's row visibly on screen. Opening a fold preserves preceding context when it fits and otherwise scrolls by the least that shows the block and its cursor."
---

One rule places the window and one count feeds it: `picker.follow` reads the
same line costs the frame spends on rows, headings and explanations, so the
cursor's row is always drawn.
