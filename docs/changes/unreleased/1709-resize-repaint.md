---
kind: fixed
title: a terminal resize leaves no text from the previous frame
pr: 1709
surface: [chat, docs]
invalidates:
  - "After the terminal changed size, cells from the previous frame could stay on screen — leftover setup prose in the first greeting, Home's panels inside Sessions or beside a chat reply. When a resize burst settles the surface now repaints the whole screen once."
---
