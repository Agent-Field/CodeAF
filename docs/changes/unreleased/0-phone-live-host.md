---
kind: added
title: A running app can pair a phone and open one test ask
surface: [app, engine]
invalidates:
  - "A running codeaf app could not export a phone pairing grant, and it could not open a durable ask without a model call. `codeaf phone pair` exports the grant, and `testing.open_ask` opens the ask when CODEAF_PHONE_FIXTURE=1 is set."
---

`codeaf phone pair` dials the running app and prints the grant. `codeaf app
--phone-fixture` sets `CODEAF_PHONE_FIXTURE=1` and opens one durable ask with
no model call. Without that switch, `testing.open_ask` is an unknown method.
