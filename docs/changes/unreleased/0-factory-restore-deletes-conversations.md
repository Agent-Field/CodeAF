---
kind: changed
title: Bringing back a dropped factory job deletes its earlier conversations, notes and step edits
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: Bring back (`POST /v1/jobs/{id}/restore`, `factory.job_restore`) kept the job's conversation: its agent's conversation (`Talk`), the replicated thread, what the runner had heard there, the person's notes for the steps, the changes made to its steps, and its crew (`Stream.Room`) with every step's conversation, so the job's chat reopened on the old conversation and a fresh run joined the old crew, found by its name. Is true now: Bring back takes `Talk`, `Thread`, `Heard`, `Notes` and `Adapted` off the job and sets its steps to the repository's recipe for its kind, then deletes, on the factory's own machine, the crew the factory made for the job (its recorded `Stream.Room` when that is the factory's here, else the factory-made item team holding its recorded conversation) and every conversation the factory recorded for it: its agent, each step's `Phase.Chat`, and every member of that crew stamped `factory_stage`. The job's next `factory.job_chats` makes a new agent and its next run a new crew."
  - "Was true: a Bring back never failed on the job's pipeline file. Is true now: the repository's recipe is read first, and a pipeline file that cannot be read refuses the Bring back with its error and leaves the job dropped."
  - "Was true: nothing Bring back did could leave a note on the job. Is true now: when a conversation or the crew cannot be deleted, the job is still brought back and its activity says `brought back · its earlier conversations were not all deleted: <why>`; a crew that cannot be deleted is closed instead, so no fresh run or new agent joins it."
---

The owner's decision: Bring back means fresh. A job that comes back shows no
earlier conversation anywhere: not in its chat, not in any step's chat, and
not in what its next run's agents read. The model a person chose for the job
stays, as its budget and priority do: it is a setting, not something said.

Only what the factory recorded for that one job is deleted, through the
engine's own doors (`teams.Delete`, `session.DeleteConversationUnder`). A
conversation a person put in the job's crew themselves leaves the crew and is
kept; another job's crew and a person's own crews are never read. A crew with
another crew inside it is closed, not deleted. A team factory's job that ran
on another runner keeps its conversations on that runner.
