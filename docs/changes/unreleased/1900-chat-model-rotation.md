---
kind: added
title: switch between saved chat models directly beside the composer
pr: 1900
surface: [chat]
invalidates:
  - >-
    Switching chat models required opening the model picker or typing a model
    command. A saved rotation now adds an exchange control beside the chat model;
    a click or f3 advances it without disturbing the draft.
  - >-
    Settings could select one chat model but could not hold a quick-switch list.
    Models now has an ordered multi-selection row; entries persist across launches
    and never change task models.
---
