---
kind: added
title: A question that asks for several figures names each one in the app
surface: [engine, app]
invalidates:
  - "An app ask carried only a title and a reason, so a model question with named blanks (input kind blanks) reached the app as one 'Your answer' box and the person had to guess which numbers were wanted. asks now carry fields: [{label, kind?, default?, choices?}] in the asker's order; the app draws one labelled box per field and answers with one 'label: value' line each in asks.answer text."
---
