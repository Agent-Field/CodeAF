---
kind: changed
title: first-run setup groups models and spending before the first conversation
pr: 680
surface: [chat]
invalidates:
  - >-
    First-run setup used separate connection, crew, and spending steps, with daily,
    per-plan, and per-conversation amounts on the spending screen. It now uses
    connection followed by one Models and spending screen with daily limit, chat
    model, and work crew controls; other spending scopes remain in settings.
  - >-
    Onboarding did not offer a chat-model control alongside the work crew. It now
    shows the model in use and provides a searchable viewport over the available
    catalog, using the existing settings writer and configuration precedence.
  - >-
    Setup had no separate capability demonstration beside its form. At 112 columns
    and wider it now includes a labelled, bordered example panel whose short preview
    settles after one play or when typing begins; narrow and screen-reader layouts
    retain their supported static behavior.
  - >-
    The first empty conversation led with the wordmark and model information, and
    its greeting disappeared on the first keystroke. A first-time conversation now
    leads with a question, the folder, and three starters that fill an unsent draft;
    its composer stays in place while typing, while returning-user greetings keep
    their existing presentation.
  - >-
    The onboarding crew chooser inherited preset descriptions that included a
    pennies-a-day claim. Its descriptions now explain the preset choice without
    a cost promise, with model details available on request.
---

The three controls open on effective settings. Custom crews and existing values are
preserved, Back retains pending edits, and cancelling a chooser does not select its
highlighted row. Memory, permission prompts, the task countdown, and spending defaults
are unchanged.

The compiled chat manual and docs/design/onboarding/DESIGN.md describe the flow, scope,
responsive layout, and example behavior. A real-terminal GIF and narrow capture accompany
the draft review.
