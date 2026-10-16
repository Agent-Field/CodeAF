---
kind: added
title: codeaf app says which kind of crew decision an ask is
pr: 0
surface: [engine]
invalidates:
  - "Was true: a crew decision on the person was an ask of subkind `crew_decision` whatever the packet was, so telling a conflict from a question, a cap, a judgement or a closing report meant joining the ask to a `decision.opened` event that is not said for packets already waiting at launch. Is true now: a `crew_decision` ask carries `decision_kind`, the packet's own kind (`question`, `conflict`, `cap`, `judgement`, `closing`); no other ask carries it."
---
