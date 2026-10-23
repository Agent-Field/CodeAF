---
kind: fixed
title: A factory job's chat spends from the job's budget, a model pick is never stale, and the chat never works in the app's folder
pr: 0
surface: [engine, app, chat, docs]
invalidates:
  - "Was true: a job's manager conversation (its chat) spent only on the person's spend ledger; only its steps' rounds counted toward the job's budget, and the chat could keep spending after the budget was reached. Is true now: every priced call the job's chat makes, its children's folded in, also goes on the job (`session.Config.JobChat`, wired over this machine's floor): on the mirror of a worker here running the job, which reports it to the factory once with the round's; on the floor's run otherwise; and before any run as `Item.ChatSpent`, which the job's `cost` reads (`factory.JobSpent`) and the next start carries into the run (also when a worker starts a queued run none of whose steps began). The ledger row stays the one record of the call; nothing adds the job's figure to it. At the budget the chat refuses its next turn with `job budget reached · $5.02 spent of $5 · raise the job's budget to go on` (under `session.ErrSpendRail`), except while the job waits on its budget question, whose answer is typed there."
  - "Was true: `POST /v1/jobs/{id}/model` (`factory.job_set_model`) checked `expect.version` like every job write, and the first model pick on a job whose chat had just been opened was refused as stale, because opening the chat keeps it on the job and moves the version. Is true now: the model route accepts `expect` and does not check it; a Done job still refuses (`conflict`). The app still sends the version and nothing about the wire changed."
  - "Was true: a job's chat with no known checkout worked in the folder its window was opened in, which in the app was the app's own home, and a manager there once cloned the repository into it. Is true now: it works in the checkout, else the factory's clone (`~/.codeaf/v3/factory/repos/<owner>/<name>`) when there is one, else an empty folder of the job's own (`~/.codeaf/v3/factory/chats/<id>`); and its brief says what the person says before Start is the brief, the job starts only when asked, and the runner clones the repository, never the manager."
---

Everything done for the job spends from the one budget (the owner's ruling).
Bring back clears the chat's pre-start figure with the rest of the run, so a
fresh job has spent nothing.
