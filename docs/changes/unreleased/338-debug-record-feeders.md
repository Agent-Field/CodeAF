---
kind: fixed
title: AFORGE_DEBUG records the calls, the tools and the choices, not just a header
pr: 338
surface: [chat, engine, docs]
invalidates:
  - "`Recorder.Call`, `Recorder.Tool` and `Recorder.Decision` had no callers outside their own tests: `AFORGE_DEBUG=1` opened a run folder, wrote `run.json`, and then recorded nothing. Model-call bodies, tool calls, and routing, hedge and effort choices now write into that folder whenever the switch is on."
  - "The manual said the call bodies, the tool calls and the choices a run made were still landing one at a time, and that a failed call's answer was still only in the model-call log. The record now holds them: `calls/<id>.json` for each model call, and `events.jsonl` for each tool call and each choice."
  - "`AFORGE_CALL_LOG_BODIES` was the only place bodies went, and the debug record was where they were going to move in a later change. They live in the run's folder now whenever the debug switch is on; the old pin still also writes them onto `calls.jsonl` for one release."
---

The recorder was built to sit on the hot path as a nil no-op. The feeders were
never wired, so the switch was honest about the folder and silent about the run.
The three methods are filled at the doors every call already passes through: the
adapter's one log writer for bodies, the tool chokepoint for the belt, and the
lane, hedge and effort stamps for the choices.
