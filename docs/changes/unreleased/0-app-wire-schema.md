---
kind: added
title: codeaf app --schema prints the wire as JSON Schema from its Go types; moves answer at once
pr: 0
surface: [engine]
invalidates:
  - "Was true: the app's wire was described only by PROTOCOL.md, written by hand, so a field's type could drift from what the server sent (settings sent `dock_badge` as a boolean while a copy of the spec said a word); a standing order's run waiting on the person was not an ask (not in asks.list or the Inbox); a crew proposal's ask carried only words and was lost on restart; `agents.move_to_task` answered only once the step in flight ended, which was minutes when a reading had been handed to a quick task, did nothing while a proposed task's card was up, and left two notices, one saying the move was the harness's idea. Is true now: every method is built from its params and result types and every event has its data type, `codeaf app --schema` prints the JSON Schema generated from them, a test fails when PROTOCOL.md's method and event index and the server disagree, and the test recorder fails any event whose data is not its contract's type; a waiting run is an ask `order:<run id>` (subkind `order_run`) everywhere asks are, closed when it stops waiting, and its result line lands in the order's chat; a proposal ask carries `proposal` and is asked again after a restart; a move answers at once (`{moving: true}` or `{task}`), lets a pending reading go, starts a proposed task, and leaves one notice."
---
