---
kind: changed
title: Clear settings categories and AI team navigation without resetting preferences
pr: 1792
surface: [chat, engine, docs]
invalidates:
  - "Chat settings used ten mixed tabs. Nine purpose-based categories now use a sidebar in wide terminals, a compact bar in narrow ones, searchable advanced controls and service options."
  - "Teams could imply human collaboration and Sessions obscured delegated work. The navigation now says AI teams and Activity, keeps Memory visible and preserves numbered shortcuts, typed aliases and saved visit keys."
  - "Resident-only controls and unwritable legacy model slots appeared in chat settings. They are omitted from this surface while their stored values and the resident registry remain intact."
  - "Hover descriptions could move controls and mouse clicks could reach behind editors. Details now occupy stable rows; editors protect their input and retain invalid values for correction."
  - "The document-reader preference was saved but not carried into the live chat engine. New CLI launches now pass the resolved preference into the session and its tasks."
  - "Show hints formerly appeared as a negative disable question. The positive label preserves the existing persisted preference; profile keys and unknown fields are not migrated or reset."
---

Settings explain scope, environment overrides and known activation boundaries.
Already-open chats still need a CLI restart for settings captured at startup,
including memory activation. The built-in manual describes those limits.
