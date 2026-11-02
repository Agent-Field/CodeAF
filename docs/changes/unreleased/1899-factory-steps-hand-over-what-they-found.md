---
kind: fixed
title: A factory step's brief carries the plan whole, every earlier step's report and every claim so far
pr: 1899
surface: [engine, chat, docs]
invalidates:
  - "Was true: a factory step's brief carried each earlier step as one line, `write: done · 3 claims · <the first 120 characters of its output>`, so a plan written in `stage_result`'s output reached write as its first sentence, and proof never saw what earlier steps claimed. Is true now: every step after plan has `the plan (from the plan step):` with the plan's output whole up to 6,000 characters (a longer one is cut and its heading says so); `before this stage:` has `- write: done · 3 claims` with up to 1,000 characters of what the step said under it (plan's line says `its plan is above`); `claims made so far:` lists every earlier claim with its step, `ok` or `not ok`, and its evidence, and proof is told to confirm or correct each rather than invent new ones; write's brief lists `files the plan names:` (up to 20, the same reading the job's planned files come from). All of it is held to about 12,000 characters, the oldest steps' words cut first (`what it said is cut to fit this brief`), then the oldest claims, and the plan never. Notes are carried in `notes:` as before."
  - "Was true: earlier steps were named from the item's stages before this one, so an approve step between plan and write (which reports nothing) left every name unmatched and the brief said `stage 1`. Is true now: the runner names each earlier result by the run's phase it came from (`run.Job.PriorNames`)."
  - "Was true: the plan step was told nothing about where its plan goes, and `stage_result` asked for output as `what you did, in a few lines`. Is true now: the plan step's brief says `Write the plan in output: what you'll change, in which files, and how you'll know it works.`, and the tool asks for output as what the step did and found, which the stages after it read."
  - "Was true: an issue's work-tree line said `git diff HEAD` and `git log` show what earlier steps changed, which missed every committed change. Is true now: once a step has run, the line says the work tree is the source of truth for what earlier steps changed, with `git log <start>..HEAD` and `git diff <start>` from the commit the run began on (bare `git log` and `git diff` for a pull request)."
  - "Was true: the worker's pull carried each step's `result` line only, so a job that moved to another worker was rebuilt with every earlier step not done, no notes and no claims. Is true now: the pull's `steps[].report` is the step's whole last report (`done`, `findings`, `claims`, `output`, `exit`, `notes`, `cost`, `chat`, the same shape as a worker report's `steps[].result`), and the runner reads it in preference to `result`. A reader's job (`GET /v1/jobs/{id}`, the queue) never carries it, `result` is still sent, and a pull without `report`, from an older factory, is read from `result` as before."
---

The plan is the most important hand-over in the pipeline, and it barely
survived: a write step saw the plan's first line, worked from the issue alone,
and proof could not see what it was meant to prove. A runner that took a job
over lost even that the plan had finished. Each step now hands the next what it
found, within a fixed size, and the job carries it across machines.
