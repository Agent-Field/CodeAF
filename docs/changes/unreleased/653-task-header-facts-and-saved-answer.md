---
kind: fixed
title: Group task header facts and preserve saved design answers
pr: 653
surface: [chat]
invalidates:
  - "Task metadata was one undifferentiated sequence. Roomy headers now separate outcome and activity from model, effort and cost on the same row, retaining the compact priority fallback."
  - "A long breadcrumb could consume the Back target even when folding ancestors could leave it visible. The current task remains whole while the header reserves Back where possible."
  - "A stopped design task could show an empty conversation because an old progress notice suppressed its saved report. The report is now shown when the conversation is empty."
---

Header padding grows in separate steps as the terminal gets taller, preserving
reading space. Pointer targets follow the same facts and breadcrumb geometry.
