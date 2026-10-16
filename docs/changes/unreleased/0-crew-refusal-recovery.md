---
kind: fixed
title: A pinned or one model held back by a refused key or empty account recovers on a new key or credit
pr: 0
surface: [chat, engine]
invalidates:
  - "A crew route refused for its key or for payment was held back until a later call on it answered. A pinned role or the one model is never sent to such a route, so reconnecting or adding credit changed nothing. Now a refusal is lifted by a different credential than the one refused (a print taken at dispatch, CrewDispatchFor / LogCrewRouteSent; router CrewRecord.fail_cred), by a credential change after an unprinted refusal (profile crew-credentials.json: print and since per provider, written by WriteAPIKey, WriteSources, ConnectCodex and the app's keys, and when route health reads; a first sighting is a baseline), or for payment by a positive balance read after the refusal. The same key written again lifts nothing; there is no timer."
  - "config.LogCrewRoute names no credential and never guesses one from the settings at response time; a caller that took a CrewDispatch when it sent the call uses LogCrewRouteSent."
  - "config.CrewAccountState had no read time. It now has ReadAt; a positive balance with no ReadAt, or read before a payment refusal, lifts nothing."
---
