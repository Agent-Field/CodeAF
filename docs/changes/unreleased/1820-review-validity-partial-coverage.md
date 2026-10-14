---
kind: fixed
title: review rejects malformed consistency claims and retains partial coverage
pr: 1820
surface: [chat, engine, docs]
invalidates:
  - "A negative consistency claim could omit its explanation and code location and still become a finding. Such output now ends with a non-postable failure record, and posting refuses malformed older findings."
  - "A partial run warned in its account but saved JSON omitted the coverage facts and its posting payload could still approve or recommend a clean merge. New reports persist their limits, keep the warning in the posting payload and avoid those claims."
---

Assembled partial reviews still finish successfully. Invalid consistency claims do
not. Models and limits are unchanged; older reports with known partial metadata
need coverage facts and a posting warning before posting. This repair does not
resolve the remaining lens session deadlines or establish complete live acceptance.
