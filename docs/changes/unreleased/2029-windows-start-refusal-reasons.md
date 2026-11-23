---
kind: fixed
title: A command Windows refuses to start says whether Defender or the length limit stopped it
pr: 2029
surface: [engine, docs]
invalidates:
  - "A command Microsoft Defender blocked on Windows failed with only \"Access is denied\". The result now leads with Microsoft Defender as the likely cause, points at Protection history, and asks the agent to tell the person rather than reword the command past the block."
  - "A command past Windows' 32,767-character command-line limit failed with \"The filename or extension is too long\". The result now names the limit and says to run the script from a file."
---

Only a refused process start on Windows changes: error 225 or 226 (antivirus
block), 5 (access denied) and 206 (command line too long). The bash tool's
result and a background job's start error lead with the reason, which fits the
step summary, then one line of advice, then Windows' own error. Every other
start failure, and every start failure on macOS and Linux, keeps its wording.
