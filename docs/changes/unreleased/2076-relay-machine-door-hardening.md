---
kind: fixed
title: a stranger can no longer hold, squat or flood a machine reached through the relay
pr: 2076
surface: [remote]
invalidates:
  - >-
    A relay stream had no deadline, so the serving machine's 30-second
    handshake limit never applied: silent dials could hold all 16 of a
    machine's slots, and one byte could burn its pairing code and freeze
    pairing. Streams now end at the handshake deadline.
  - >-
    The hosted relay kept its rate limits per machine name. It now limits each
    caller network at the front door (30 dials and 10 registrations a minute)
    and refuses names codeaf cannot produce before any object is created.
  - >-
    A registration with a different key could replace a live machine that
    stalled for five seconds. Only the same key may replace a silent one.
  - >-
    codeaf serve printed "lost sync — trying again" and then nothing when it
    recovered. It now answers with "back — reachable as <name> again".
---

Found by a review of #2073 after its machine doors went live on the hosted
relay, and checked by hand against the Worker run locally.
