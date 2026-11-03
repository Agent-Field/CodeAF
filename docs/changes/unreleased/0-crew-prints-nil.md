---
kind: fixed
title: Crew watches share complete map initialization
pr: 0
surface: [app, engine]
invalidates:
  - Cold-sweep test watches previously omitted writable crew maps and could panic when a changed input triggered reconciliation; all watches now use the production constructor.
---

Production, test, and benchmark crew watches now share one constructor. It
initializes crew fingerprints, member fingerprints, traffic cursors, action
tracking, packets, asks, holds, opening state, wake stamps, and recovery state.
A changed input can safely rebuild a watch created for quiet polling.

The nil fingerprint map was traced to the cold-sweep test fixture. Production
already initialized it. The exact input-stamp change in the original intermittent
failure was not captured. No copy, restore, or map-clearing path was found.
