---
kind: fixed
title: a check that answers while the send keeps failing now waits, and a media wait names its model
pr: 683
surface: [chat]
invalidates:
  - "Connection recovery was described as bounded, and in wall clock it was — two minutes — but nothing bounded what it SPENT inside that window. A check that answered while the request itself still could not go out left no pause between one send and the next: measured on dev@5ffb15b18, a two-minute chat fixture sent 7,975,780 requests and wrote 15,951,560 model-call log rows, and a twenty-second media fixture sent 3,728,757. Every send after the first recovered pass now waits backoffFor its own count, and the passes are counted and capped; the same fixtures send 16 and 6."
  - "The media road passed an EMPTY model to the reachability wait, so every phase report a picture, video, speech or transcription request made while its connection was gone was thrown away by internal/tui3's PostPhaseNews, which drops a phase for a model nobody named. The wait now carries the model the call was made with. It is still not DRAWN on the conversation row: tui3's livePhase reads the phase of the window's own model, and a picture is drawn by a different one."
  - "Connectivity recovery resumed immediately on every recovered pass, described in retry.go as `a fresh connection gets the request immediately, without old backoff`. That now holds for the FIRST recovered pass only. It still spends none of the provider retry ladder."
---

`recoverBeforeSend` in `internal/provider/connectivity.go` is the one primitive
both roads call, so the chat road and the media road cannot drift on how long a
pre-send failure waits. The two-minute window is still what ends a real outage;
`connectionRetryPasses` is the arithmetic backstop for the degenerate case the
window cannot bound, where an origin answers a check in microseconds while
refusing every send and the wall clock barely moves.
