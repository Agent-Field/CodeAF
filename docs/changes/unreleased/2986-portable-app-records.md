---
kind: fixed
title: App packages carry their shared record dependencies
pr: 2986
surface: [app, engine]
invalidates:
  - An app package that used another app's record types could fail in a fresh profile.
  - Two installed apps with incompatible private record types of the same name could prevent the second app from applying.
---

Pack includes the record types and fields that an app needs and checks that a fresh profile can open them. Install shares matching Person and Company types. Apps can keep incompatible private definitions under their own record names.
