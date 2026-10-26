---
kind: changed
title: Make the add-device card clearer and clickable
pr: 1968
surface: [chat, docs]
invalidates:
  - "The add-device heading was passive text. It now highlights under the pointer and opens or closes the pairing instructions when clicked."
  - "The add-device description repeated its title and the instructions had uniform emphasis. The card now separates the description from the steps and emphasizes the command."
---

The heading, supporting text, command and key hints use the existing theme's
text hierarchy. Only the heading's visible cells respond to the pointer;
description text and surrounding space stay passive. The keyboard shortcut
and pairing approval flow are unchanged.
