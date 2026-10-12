---
kind: added
title: The app can launch a guarded local codeaf engine
pr: 1800
surface: [build, remote, docs]
invalidates:
  - "The app-v2 API was entirely a design. codeaf app serve now opens its loopback transport with health and shutdown routes; agent, key and factory routes remain later lanes."
  - "A local app bridge could inherit the terminal's state root and token override. The app serve launch uses its own app data home and a fresh in-memory token every time."
---

A1 ports the bridge's origin/host boundary and token generation into
`internal/appapi`. One stdout hello names the allocated listener, credential,
pid, build, contract, home and independent epoch. Stdin EOF, parent death,
signals and authenticated shutdown all end the launch; logs stay on stderr
and in the app home without copying stdin credentials or the bearer token.
