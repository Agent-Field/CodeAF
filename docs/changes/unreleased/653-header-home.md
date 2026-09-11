---
kind: changed
title: Give conversation navigation a padded header and a Home button
pr: 653
surface: [chat]
invalidates:
  - "The tab strip occupied the very first row with only the active tab filled. Tabs now have individual backgrounds and the selected tab has a contrasting surface; roomy terminals add space above and below the labels and after task breadcrumbs."
  - "Home was reachable only through keyboard or page navigation. A Home target now precedes conversation tabs where the connection and available width support it. It preserves existing and new-chat drafts."
---

Short frames collapse vertical padding. Drawing, scroll budgets and pointer targets
share the header geometry, so blank rows cannot trigger the conversation below.
Home supports hover even without color and leaves Space Space available.

The filled tab includes an inner cell before its status mark and after its close
button. Both belong to their targets; the gap between tabs remains inert.
