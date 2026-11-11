---
kind: fixed
title: Explicit local Run starts factory reads and configured repository rules
pr: 1856
surface: [engine]
invalidates: []
---

The native app’s owned factory now polls GitHub, reads items with the configured model, and runs repository start-rule intake after explicit Run consent. Previously the app runner did not run intake, so those rules could not start jobs there. Launch, setup and cold resume stay quiet. Matching repository rules may start jobs without another click after Run; the model only reads and proposes. Repeated Run creates no duplicate loops, and joined factories gain no local polling authority.

Quitting cancels and waits for reads before releasing the factory. Closing can wait for an in-flight request. Rules and CODEOWNERS refresh use the signed-in caller’s GitHub access, including the existing intake refresh cadence while reading and idle. An unavailable saved one-model policy makes no fallback call.
