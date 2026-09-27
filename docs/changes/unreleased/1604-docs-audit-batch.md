---
kind: fixed
title: A batch of fixes from the docs audit — headless, tasks, standing, teams, runs, media, remote
pr: 1604
surface: [chat, engine, remote, docs]
invalidates:
  - "A turn waiting on the daily-budget card could outlive cancellation, and `codeaf do --yes-spend` workers could wait on that card despite the run being authorized. Cancelled waits now close their streams, and child workers and checkers inherit only the run-scoped daily-limit permission; task, time and approval limits remain in force."
  - "`codeaf do \"\"` (or an all-blank brief) ran a paid job on a goal the planner invented. It is refused with the same usage as `codeaf do` with no argument, before anything is spent."
  - "Flags after the brief (`codeaf senior-dev \"<brief>\" --max-cost 0.5`) were folded into the brief and the default ceiling applied. They are parsed wherever they sit for every delegate program, an unknown flag is refused, and `--` still ends flags."
  - "`codeaf do --json` on the run engine reported zero tokens beside a non-zero spend. The run engine's workers now carry input and output tokens to the receipt."
  - "A read hand-off (`◆ reading: …`) ran as an ordinary quick task with bash and commit, so it could write, commit and merge. It now gets the audit read-only belt, hands anything it cannot do back to the conversation, and a failed hand-off leaves no stopped card."
  - "A task held by the busy-machine gate had already taken its folder lock and branch, and `/stop` could not end it. A held task owns only its plan row (`queued · machine busy`), stops at once, and prepares its folder only after admission; an admitted task's refusals are unchanged."
  - "After an engine restart ordinary planned tasks stayed working without a driver. Reopening reconnects the same plan and recorded copy, or readmits the exact queued request before preparing any folder; missing older ground becomes explicitly interrupted, never done. `/stop` also reaches a sole queued plan from the main box."
  - "An explicit stop was recorded as a failed crew, so the crew line said `failed … /redo stronger` and the router learned a failure. A stop reads `stopped`, offers no redo and teaches the router nothing."
  - "Plan-born task workers were briefed without the project's standing orders. Every worker a run seats carries the one standing section."
  - "A project standing order stored with a trailing separator was listed under other projects; the standing card quoted the allowance the session started with; a firing that ran a task read `said:`. Paths are compared cleaned, the card quotes the live allowance the rail enforces, and task firings read `task:`."
  - "A manager started with `M` had no handle until its first turn, so `team_send` could not reach it. It gets a unique fallback handle when it is registered."
  - "A sub-team started by `team_start` did not inherit the starting manager's approval posture, and the start card showed the rule word `default`. The posture is carried to the child, and a fallback rule shows the cost clause."
  - "`team_start`'s description said the person is always asked first, so a manager under an allowing posture reported an approval nobody gave. It now says approval follows the posture."
  - "Every check in a fan-out run drew from one checker-seat tally, so later checks stopped at a ceiling other checks had spent, and the run still said done. Each check has its own ceiling, and a run with unfinished checks does not end done."
  - "A run branch kept on a protected or moved checkout was invisible to `/land`, and `.orig` backups could reach a task branch. `/land` lists and lands kept run branches, and `.orig` files are droppings."
  - "A Home conversation could be opened on another project's live engine. A conversation whose engine answers for a different workspace is refused visibly before anything is sent."
  - "The wall showed other windows' conversations as open here, and a tile opened from Teams landed back on Teams. It shows only this window's conversations, and a tile lands on its conversation."
  - "`/memory`, `/remember`, `/memories` and `/forget` said memory is off on the hosted engine road. They reach the engine's memory over the wire."
  - "Generated JPEG bytes were saved under a `.png` name. Images are named by their sniffed bytes."
  - "Speech spend had no role in usage.jsonl and media calls wrote no call-log rows. Speech records under its own role, and media requests write a call-log pair with the provider's cost."
  - "After a `--host` redial the window became a watcher of its own dead pipe, and ssh stderr painted over the frame. The same window takes its keyboard back by client id, and a redial's stderr goes to the diagnostic tail."
  - "`/drafts` drew nothing while it held the keyboard, `/skill` on an empty shelf left `/skill ` in the box, and esc could not cancel a pending browser sign-in. All three are fixed."
---

One entry for the whole batch. The issues it closes, the ones it only narrows, and the ones left for a ruling are listed in the pull request.
