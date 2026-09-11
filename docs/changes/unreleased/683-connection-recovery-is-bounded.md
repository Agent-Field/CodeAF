---
kind: fixed
title: a check that answers while the send keeps failing now waits, and a media wait names its model
pr: 683
surface: [chat]
invalidates:
  - "Connection recovery was described as bounded, but a check that answered while the request itself still could not go out left no pause between sends: measured on dev@5ffb15b18, a two-minute chat fixture sent 7,975,780 requests and wrote 15,951,560 model-call log rows, and a twenty-second media fixture sent 3,728,757. PR #858 later replaced the retry budgets with control.Plan.Deadline, but the connection branch still decremented the dispatcher's attempt and therefore never reached that deadline or its wait. It no longer cancels the pass: the first recovered send is immediate, every further one pays the ordinary growing backoff, stubbed waits are charged to the plan, and the plan's one deadline ends the call."
  - "The media road passed an EMPTY model to the reachability wait, so every phase report a picture, video, speech or transcription request made while its connection was gone was thrown away by internal/tui3's PostPhaseNews, which drops a phase for a model nobody named. The wait now carries the model the call was made with. It is still not DRAWN on the conversation row: tui3's livePhase reads the phase of the window's own model, and a picture is drawn by a different one."
  - "Connectivity recovery resumed immediately on every recovered pass. That now holds for the FIRST recovered pass only. A pre-send failure still implicates no serving machine, so it spends no machine move or request-shape rung."
---

Completion recovery now stays inside the one loop in
`internal/provider/dispatch.go`: `control.Plan.Deadline` is its only budget, and
the existing `owed` accounting makes that deadline reachable even when a test
clock barely moves. Media has no completion plan; its allowed media request loop
uses the caller's deadline or the existing two-minute connection window and
charges the same growing waits rather than adding an attempt cap.
