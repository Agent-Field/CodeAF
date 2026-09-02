---
kind: fixed
title: a lane that attaches mid-job is handed the live job, not the checkpoint stub
pr: 471
surface: [chat]
invalidates:
  - "A lane that attached while a job was still running rebuilt that job from the checkpoint row, so the clock stayed blank and the page had no command until the process ended. Replay now publishes the live registry notice when the job is still in it."
---
