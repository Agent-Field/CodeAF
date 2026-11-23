---
kind: fixed
title: A session test agent now waits for the save its receipt wait starts, which fixes the D3 red
pr: 2698
surface: [build]
invalidates:
  - "TestD3AgentCloseWaitsForTheReturnedCommandsCapture failing in the Full check was thought to be a store daemon the test left running, or a Close that did not wait for its cancelled capture. It was neither: the test fixture's own receipt wait starts one more save on a seat that pipelines its saves and nothing joined it, so the temporary home was removed under a seal still rewriting the call log. The fixture now drains the seat after the receipt wait."
---

Test-fixture change only. TestPlandbCliWritebackSkipsTheWorkerSOwnDone and
TestPlandbCliDepthFloorCancelsItsUndeliverableChildren fail the same way in the Full check and are
not this: they use the default host seat, so there is no save for the fixture to join, and the cause
is still open.
