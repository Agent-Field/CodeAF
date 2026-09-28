---
kind: added
title: Run non-interactive shell commands with ! and see their output live
pr: 1654
surface: [chat, engine, remote, docs]
invalidates:
  - "A leading ! was ordinary model input. Home and conversation composers now run it as an explicit human shell command in the selected project, without requiring a model or provider key."
  - "The composer used the same prompt for every draft. A leading ! now changes it to an amber dollar prompt, and deleting the prefix restores the normal prompt."
  - "Shell output normally arrived as a bounded tool preview. Human shell commands now show literal stdout/stderr while running, retain the bounded result in conversation history, and wait for the next message before the model responds."
  - "Remote protocol 19 had no human shell submission door. Protocol 20 adds SubmitBash and streamed output events; ordinary and automated Submit calls never gain shell authority from a leading !."
---
