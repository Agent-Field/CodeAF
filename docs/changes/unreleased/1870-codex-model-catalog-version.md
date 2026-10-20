---
kind: fixed
title: Codex discovery lists newer models available to the signed-in account
pr: 1870
surface: [chat, engine]
invalidates: []
---

Update the shared catalog client version from 0.144.1 to 0.162.1. The account
backend gates model visibility by client version, so the older version omitted
newer models even for accounts with access. Direct listing and translated
catalog requests use the same version; account permissions and visibility
filtering are unchanged.
