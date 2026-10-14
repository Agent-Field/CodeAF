---
kind: added
title: App model lists report the resolved conversation model and known prices
pr: 1800
surface: [engine]
invalidates:
  - "The app model list exposed only the saved default, which was empty on a fresh profile. It now also reports resolved, the saved talk model or the built-in default."
  - "App model rows had no prices. They now include known input and output tariffs in USD per million tokens, preserving published zeros and omitting unknown prices."
---

The existing default field keeps its saved-choice meaning. Prices come from
the spend meter's installed catalogue tariff or known fallback rows, without
adding a network request. Generated JSON Schemas and TypeScript publish the
same additive fields.
