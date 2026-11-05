---
kind: fixed
title: Retry lost opening planner replies before failing the crew
pr: 0
surface: [engine, chat, app]
invalidates:
  - "Cut-off opening planner replies and JSON repairs that lost all nodes could be treated as deliberate empty plans. They now trigger one fresh planning attempt and retain the cut-off reason if recovery fails."
---

Blank opening replies also trigger recovery. The retry requests at least 8,192
output tokens, or twice the reported completion tokens, through the existing
provider and spending checks; no existing limit constant changes. Exhausted
recovery ends as failed with `the planner's reply was cut off`, saved once as an
assistant notice using the empty-crew recovery path from #2403. A complete
explicit empty opening keeps that path's own reason. Later planner calls keep
their existing behavior.
