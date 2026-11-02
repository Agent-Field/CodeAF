---
kind: changed
title: Dropping a factory job deletes its conversations; Bring back is a fresh Backlog job
pr: 1899
surface: [engine, chat, docs]
invalidates:
  - "Was true: dropping a job deleted none of its conversations: its agent's conversation (`Talk`), every step's, and the crew the factory made for it stayed on the factory's machine, and the job went on naming them. Is true now: `POST /v1/jobs/{id}/drop` (`factory.job_drop`), once the job is dropped and its local branch and folder deleted, takes `Talk`, `Thread`, `Heard`, its crew (`Stream.Room`) and every step's and round's chat (`Phase.Chat`, `Phase.Handle`, `RoundLine.Chat`) off the job, then deletes, on the factory's own machine, the crew the factory made for the job (its recorded `Stream.Room` when that is the factory's here, else the factory-made item team holding its recorded conversation) and every conversation the factory recorded for it: its agent, each step's latest chat and each finished round's, and every member of that crew stamped `factory_stage`. A conversation a person put in that crew leaves it and is kept; another job's crew and a person's own crews are never read. A crew with another crew inside it is closed, not deleted. The job keeps its steps, spend, notes and step edits until Bring back. A team factory's job that ran on another runner keeps its conversations on that runner."
  - "Was true: a drop answered as soon as the job was marked and its folder deleted. Is true now: it also waits for the conversations' deletes, each of which first stops a turn still running in it (up to 30 seconds a conversation, after the folder's wait for this machine's runner to let go, about 8 seconds)."
  - "Was true: nothing a drop did could leave a note about conversations. Is true now: when a conversation or the crew cannot be deleted, the job is still dropped, no longer names it, and its activity says `dropped · its conversations were not all deleted: <why>`."
  - "Was true: Bring back (`POST /v1/jobs/{id}/restore`, `factory.job_restore`) kept the job's conversation, its notes and its step edits. Is true now: Bring back takes `Talk`, `Thread`, `Heard`, `Notes` and `Adapted` off the job and sets its steps to the repository's recipe for its kind. Before that, while the job is still dropped and so cannot be started, it deletes again the job's local branch and folder (anything re-created since the drop) and whatever conversations and crew the job still names, which is nothing for a job dropped after this change and everything for one dropped before it. A delete that fails says `brought back · the local branch and folder were kept: <why>` or `brought back · its conversations were not all deleted: <why>` on its activity, and the job is brought back all the same."
  - "Was true: a Bring back never failed on the job's pipeline file. Is true now: the repository's recipe is read first, and a pipeline file that cannot be read refuses the Bring back with its error and leaves the job dropped."
  - "Was true: a manager's doors still changed a dropped job: `factory_run` edited its steps, `factory_item`'s card changed its steps, thinking, budget or notes, and `factory.job_chats` made the job a new agent when it had none. Is true now: each refuses a dropped job with the start's own sentence, `#12 was dropped · Bring back returns it to Backlog` (`factory.job_chats` answers it as `not_found`). `factory.Edit` holds the refusal, so every edit of a job's steps does. `internal/factory/run.DroppedRefusal` moved to `factory.DroppedRefusal`, beside `factory.RefuseDropped`, which every one of those doors and the launch read."
---

The owner's decision: Drop throws everything of the job's work away, its
local branch, its working folder and its conversations, so Bring back is
simply the job in Backlog again, fresh. What was said about a job goes with
the job; nothing of it reaches a run the job makes after it comes back.

Only what the factory recorded for that one job is deleted, through the
engine's own doors (`teams.Delete`, `session.DeleteConversationUnder`), and
the job stops naming its conversations in the same write that reads them, so
a delete that fails leaves no link to a conversation half gone. The app hears
of no deleted conversation as such: it learns of the drop from its next read
of the job, after which `factory.job_chats` refuses it, and of a deleted crew
as `crew.removed`.
