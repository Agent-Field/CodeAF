---
kind: fixed
title: codeaf app distinguishes actual execution models from selections
pr: 0
surface: [engine, chat]
invalidates:
  - "Was true: app model selectors could show a saved preference while a request ran on a fallback or resolved alias, and factory model changes could replace the apparent model of an older step. Is true now: agents publish active and last-used model identity, exact provider-reported ids, request policy, pending choices, and ordered snapshots; restored conversations retain last use without reviving an active request."
  - "Was true: fallback policy was implicit in app conversations and model changes travelled independently of policy. Is true now: app clients can explicitly choose Fixed or Automatic model selection; Fixed prevents foreground cross-model substitution, while factory model and policy choices are delivered together and inherited by later steps. Factory approval automation remains independent."
---

The additive model identity facts cross session, remote and app wires. Factory
round results use last execution evidence rather than the preference for a future
request. Existing callers that omit the optional policy retain their prior policy.
