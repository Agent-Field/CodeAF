---
kind: added
title: Control which records and fields a Personal app can use
pr: 2961
surface: [app, engine, chat]
invalidates:
  - App grants used to authorize a whole record type. A type can now restrict rows by the person's identity and hide or mask fields in every app that uses the type.
  - A live connection used to have one credential for the whole app. A personal connection can now use the person's own source credential.
  - App models used to run without checks for declared data classes. An app with restricted or PII fields now requires an approved destination.
---

The app records masks and refusals without including values. Protected views
refetch when access changes. Use `codeaf app model-policy` to set the profile's
approved model destinations.
