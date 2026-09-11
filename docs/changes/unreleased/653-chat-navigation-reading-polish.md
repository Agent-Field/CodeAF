---
kind: fixed
title: Make chat switching respond to hover and give conversation navigation room to read
pr: 653
surface: [chat]
invalidates:
  - "Conversation and task transcripts started flush against the terminal edge. They now use a two-cell reading gutter where the layout has room, with link and card hit targets moved together and copied text stripped of the layout padding."
  - "Opening a fresh hosted task could leave a blank body labeled loading even when no transcript read existed. The page now shows its known instruction and activity while records arrive, and breadcrumb clicks start their transcript read immediately."
  - "An unnamed conversation used main as its tab title. Tabs and their breadcrumb roots now say Untitled until the existing title event arrives after the first completed reply. A conversation explicitly named main keeps its name."
  - "Chat-switcher rows accepted clicks but ignored mouse movement. Rows and the expansion control now highlight under the pointer without changing the keyboard choice, and an outside click dismisses the card without reaching the page behind it."
  - "The tab strip used vertical fences and bracketed colored labels. Quiet gaps now separate compact padded targets; active tabs use filled backgrounds and stronger text, while terminals without background color retain brackets. Breadcrumb ancestors remain readable and the current task has stronger text."
---

Mouse hover cancels the optional quick-switch fade while the person reads a row.
Keyboard navigation clears delayed pointer feedback. The existing navigation,
close controls, status marks and task facts keep their meanings.
