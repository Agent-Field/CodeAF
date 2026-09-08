---
kind: fixed
title: a pinned lane the router refuses for a model yields to another lane, and says so
pr: 533
surface: [chat, engine]
invalidates:
  - "A pinned lane got EVERY request for that model 'and nowhere else' — no longer true of one case. When the router answers `permits only: <lane>`, meaning that machine cannot serve that model at all, the pin is retired FOR THAT MODEL for the rest of the run: the request that collected the refusal is widened and sent again once, and every later request for that model routes as `auto` does. Nothing on disk changes, every other model still goes to the pinned machine, and pinning again puts it straight back."
  - "A chat turn on a lane the router will not serve the model from no longer dies with `the request itself was refused` and the router's own sentence on the screen. It was every turn of such a session; now the first one pays a single 404 and the work goes out on another lane."
  - "aforge now says one line in the conversation when this happens, once per (lane, model) per run: `<lane> cannot serve this model; routing on auto for this model until you pin again`. It is a note that stays, not a status-line rider — the rider is outranked by the phase clock while a turn is working and is overwritten by the answer's own news, so a sentence that lived only there was never seen."
  - "A routing refusal absorbed by the recovery ladder now strikes the machine that made it. It used to strike only when the refusal reached a caller as a 4xx, so a lane that had just said it cannot serve a model stayed in the serving set and was chosen again on the next turn."
  - "provider.SetLanePin no longer forgets what the wire said when it is handed the SAME row again. It is called by every place that resolves the row, including the standing ticker, which rebuilds a posture every five minutes — so a retirement that cleared on any call to it lasted five minutes rather than the run. Only a row that CHANGED forgets."
---

The refusal is now classified once, at the fork every routing refusal passes through, and
acted on there: the strike, the retirement of a person's own pin, and one retry at rung
one — the rung that drops the whole provider object, so the request that goes out is the
one `auto` would have sent. The walk and the ladder are both below that line, and the
walk declining is exactly the case the reported turn died in.
