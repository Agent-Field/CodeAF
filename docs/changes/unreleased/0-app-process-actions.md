---
kind: added
title: codeaf app asks the engine what a process allows and stops one agent's processes at once
pr: 0
surface: [engine]
invalidates:
  - "Was true: processes.rerun reran a record saved as running that nobody vouched for, after rewriting it to stopped, in the conversation's current workspace whatever folder it was recorded in, and a task worker's command from the conversation. Is true now: run again is only a command the conversation's own chat started, whose ending its owner saw, kept whole, in its recorded folder while that is still the workspace and exists; everything else is refused with a reason. The old Process.Rerun door holds the same policy, and the app reruns only through the stamped Process.RerunAt."
  - "Was true: stopping a task worker's process from the conversation answered 'there is no job N', though the page showed it live. Is true now: a person's stop reaches it through the engine's own owner map."
  - "Was true: a stop of a render that had already committed to saving its file told the model 'job N was stopped'. Is true now: it is reported not stopped, with the kill's own sentence, and no note is sent."
  - "Was true: the only stop-all was services.stop_all, every agent's services at once. Is true now: processes.stop_all stops ONE agent's running jobs and own services under one 10 s deadline and answers a receipt naming what it could not stop or see."
  - "Was true: a surface had to guess Stop and Run again from a row's state and command. Is true now: processes.actions answers each verb by id from the engine holding the conversation, and processes.log_target names a managed log as facts for a native opener, never as a path anything accepts back."
  - "Was true: a cancelled stamped rerun could still write an RPC and start a command, and an older engine could report a stop through an unverifiable Cancel. Is true now: expired requests are refused before launch, caller deadlines cross the private engine wire, and an unsupported stamped stop never falls back to Cancel."
---
