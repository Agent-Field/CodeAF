---
kind: fixed
title: hosted acceptance checks actual task endings and retains diagnostics
pr: 1816
surface: [build]
invalidates:
  - "Hosted acceptance treated landed files as successful task completion and could print ALL PASS while a task's durable ending was failed. Completion now requires all three handed-off tasks to end done, without changing the existing file/UI assertions or five-minute guards."
  - "A successful hosted drive deleted the full outcome and action records before they could explain a later failed-task report. Private sanitized phase and final records now survive cleanup, model-call metadata is kept inside the owned home without request/response bodies, and externally bound profiles are handled by the application without the drive reading or copying their credential file."
---
