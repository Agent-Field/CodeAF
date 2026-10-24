---
kind: added
title: A factory job says when each step and round began, and its step lines are typed and live
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: a job's `steps[]` said when a step ended (`at`) but never when it began, and a finished round in `steps[].rounds` was `{round, summary?, at}` only, so a surface placed a step's rounds from the first line of its chat. Is true now: each step carries `started_at`, when its latest round began (absent while pending, and again once sent back), and each finished round carries its own `started_at`. The runner records the start as a round begins in a fresh chat and keeps it when a round carries on in the chat a pause cut short; a worker reports it on its own clock. Jobs recorded before this have none."
  - "Was true: a round's conversation was not kept, so only a step's latest chat could be opened. Is true now: each finished round keeps its chat (`rounds[].chat`), withheld from every reader of the job exactly as `steps[].chat` is (a path on the runner's machine; only the worker holding the lease reads it), and `factory.job_chats` answers each step's `rounds: [{round, agent}]`, held open like the step's own chat."
  - "Was true: the runner's lines in a job's conversation (`review started`, `review done · 4m · …`, `asking you: …`, `stopped`) reached the app as plain `answer` entries, only when the conversation was read again, and only their words said which step they were about. Is true now: each line arrives live as `entry.added` the moment it is written, with the id a later read answers, and a line that reports one step's round carries `progress {step, round?, event}` (`started`, `done`, `waiting`, `failed`, `stopped`); lines about the job carry none. The words are unchanged, so the terminal reads the same."
  - "Was true: `internal/factory/run.Talk.Say` and `session.AppendProgress` took a transcript and a line. Is true now: both take the step the line reports on (`*factory.StepProgress`, nil for a line about the job), and `session.OnProgress` tells a reader each line once it is on disk."
  - "Was true: a runner's progress line was journaled with `presentation {audience, kind: factory-progress}` only. Is true now: a step line's presentation also carries `step {step, round?, event}`, and `session.Record.Progress` keeps each such mark beside the entries, keyed by the line's place in `Earlier` followed by `Entries[Floor:]` (where a compaction pass is placed), so it is read back after a restart or a compaction. `DisplayEntry` does not grow: it stays 296 bytes."
---

The app draws each round of a factory step as a frame of its own, and needed
three things the engine did not give it: when a step and each of its rounds
began, a way to open an earlier round's chat, and the runner's step lines as
facts rather than words. The frames now place themselves from the job read,
earlier rounds open like the latest, and the lines that say what a frame already
shows can be folded into it as they are written, while the terminal shows the
same words it always did.
