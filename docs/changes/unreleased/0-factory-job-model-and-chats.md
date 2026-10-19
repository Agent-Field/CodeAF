---
kind: added
title: A factory job has one model, changeable any time, and its chats on its own page
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: every step of every factory job ran on the runner's default model, and nothing on the wire said which model a step had used. Is true now: a job may name its own model (`job.model`, set with `POST /v1/jobs/{id}/model`, the app's `factory.job_set_model`; empty goes back to the default); every step opens on it, a step already running switches at its next turn (the `model` lease signal and the runner's SetModel), the job's agent follows it, `steps[].model` says what each step ran on, and the queue's `model` is the default a job without one runs on."
  - "Was true: a job's conversations reached the app only as a team under Crews, and nothing named which chat belonged to which step. Is true now: `factory.job_chats {factory, job}` answers the job's agent (made when the job has none yet; no model asked, no job started) and the latest chat of each step that ran, read from the factory's own record and held open for the app; it answers only this machine's factory. Teams the factory made no longer appear in `crews.overview`'s team pages."
---

The person reads one model per job and changes it the way they change a
conversation's, and talks to the job where the job is: its agent for the
brief and the steer, any step's chat to message that step directly.
