---
kind: added
title: codeaf app pages a crew's and all crews' recorded process history
pr: 0
surface: [engine]
invalidates:
  - "Was true: processes.page refused every crew or scope form as bad_params. Is true now: an engine naming processes.crew serves {crew} (the recorded subtree, closed and archived descendants included) and {scope:\"all\"}; one without it still answers bad_params."
  - "Was true: a surface could only build a crew's process list by paging each member itself. Is true now: the engine walks the recorded roster in owner rounds of five rows, says owners done/total and names unreadable owners; it promises no cross-owner order."
