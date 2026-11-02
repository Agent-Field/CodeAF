---
kind: changed
title: Turning a factory repository off stops the factory working on it, and asks before stopping its jobs
pr: 1899
surface: [engine, app, chat, docs]
invalidates:
  - "Was true: turning a repository off only removed it from the watched list, so GitHub stopped being read for it while its jobs stayed on the queue, intake could still start or ask about its labelled jobs, triage still paid a model to read them, the poll still followed their pull requests, and start, run, publish and merge still worked on them. Is true now: a repository that is off takes every one of its jobs out of `GET /v1/queue`, in every state; intake, triage and the pull-request follow pass them by; and run, start_now, run_again, a step's rerun, resume, approve, retry_publish, merge, request_changes, recheck, sync and `POST /v1/jobs` on it are refused with 409 `reason` `repo_off` and `acme/api is off · turn it on in Repos to run its jobs.`."
  - "Was true: `PUT /v1/repos/{owner}/{repo}` with `{\"on\": false}` and `PUT /v1/repos` turned a repository off whatever its jobs were doing. Is true now: while any of its jobs is running, queued or waiting on a person, both are refused with 409 `reason` `has_running` and a sentence naming the jobs (`acme/api has 1 job running (api#12) and 1 waiting on you (api#7).`), one sentence per repository for the whole list, and nothing changes. `\"stop\": true` on either body stops each of those jobs as its own Stop does (work kept, the stop recorded, Undo offered) and then turns the repository off; a stop that fails leaves it on and names the job."
  - "Was true: `factory.repo_set` took exactly `{factory, repo, on}` and the app received any refusal of it in fixed words. Is true now: it takes `stop?` too (only with `on: false`), and the factory's 409 has_running reaches the app as the new appwire code `has_running` with the factory's sentence as the message, with or without the phase marker."
  - "Was true: every repository on the queue said `\"watched\": true, \"on\": true`, whatever the store kept. Is true now: the queue lists only repositories that are on, each `watched` says whether it is on the store's watched list, and `on` is gone from the queue's repositories."
  - "Was true: a job's read had no way to say its repository was off. Is true now: `GET /v1/jobs/{id}` still answers a job of a repository that is off, with `repo_off: true` and its run, start, run again, approve, retry publish, merge, step rerun and sync controls absent."
  - "Was true: a job whose pull request waited on a person (changes requested, a conflict, a failed merge) offered Stop, and the stop refused it as not running. Is true now: such a job offers no Stop and does not keep its repository from being turned off."
---

The owner's decision: a repository that is off is one the factory does not
work on at all. Turning it back on brings every job back as it was; nothing is
deleted or made again, and the poll resumes from its cursor.

A floor that keeps no watched list (one never given repositories) has no
switch, so nothing on it is off; once a list is kept, an empty one has every
repository off.
