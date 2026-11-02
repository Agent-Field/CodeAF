---
kind: fixed
title: codeaf serve and chat --at work through the relay your chats already sync through
pr: 2073
surface: [remote, docs]
invalidates:
  - "codeaf serve and chat --at needed CODEAF_RELAY or a saved relay file; with neither they said no sync address was set up. They now fall back to the relay sync resolves (CODEAF_SYNC_URL, the relay a pairing saved, then the hosted relay)."
  - "The relay client dropped the path of a relay address, so https://host/fabric was called at https://host/v1/engine. It now calls https://host/fabric/v1/engine and /fabric/v1/dial/<name>, as every other client does."
  - "The machine carrier was only the raw aforge-relay/1 HTTP upgrade. It now rides a WebSocket offering that protocol and falls back to the raw upgrade when a relay answers 426; codeaf relay accepts both."
  - "The hosted relay had no machine doors and answered /v1/engine with 404. It now serves /v1/engine and /v1/dial/<name> through one Durable Object per machine name, which needs the v4-machines migration and the MACHINE binding in a deployment's config."
  - "codeaf serve said lost sync on every retry and quit when the relay still held its own name from a dropped connection. It now says it once per outage and waits the stale name out."
---

A relay that is down, or one that cannot connect machines, is now one sentence naming the relay and what
to try, never an upstream body such as `error code: 522`.
