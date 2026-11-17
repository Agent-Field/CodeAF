---
kind: fixed
title: Inbox items clear reliably, a resume carries Auto past its pause, one finished event wakes once
pr: 2694
surface: [app, engine, factory]
invalidates:
  - "Was true: a team's delivery cursor was not saved when it passed entries that only wake the manager, so after every relaunch the same old finished events woke the manager again. Is true now: the cursor is saved past wake entries, and a finished event wakes the manager once."
  - "Was true: a team wake the manager answered `[no change]` was carried on into a further turn. Is true now: a wake answered `[no change]` ends there, read off the transcript, so it holds for a wake the team starts as much as for a note submitted as a message."
  - "Was true: an agent's open questions stayed open after the person wrote to it, so their Inbox items stayed until each was answered by its own card. Is true now: any message from the person closes the agent's open questions, and the agent may ask again if it still needs to; a question already answered is not closed, so its receipt can still change the answer."
  - "Was true: an automation run's question stayed in the Inbox after the run stopped waiting for it. Is true now: it leaves the Inbox when the run stops waiting; a run answered Go, Skip or Not right from another surface reads as answered (\"Answered · you\"), and one that stopped waiting unanswered as no longer needed."
  - "Was true: an Inbox item could only be answered, never put away. Is true now: `inbox.dismiss {id: \"agent/askID\"}` (feature inbox.dismiss) closes its question with nothing decided; a dismissed permission reads to the agent as a denied call with the note \"Dismissed from the Inbox without an answer\", and fuel gates, library choices and items in their 5-second hold refuse it. A dismissed kept-running process's line stays dismissed across app restarts until its episode ends."
  - "Was true: a Factory job Auto paused on a failing proof or policy check could only be resumed by fixing the check. Is true now: `POST /v1/jobs/{id}/resume_auto` with the person's words (the manager's factory_answer action resume) records an override in `Item.Overrides` and carries Auto past that finding; the job publishes but does not auto-merge. The override covers only the run it was given for: every new run clears it with the job's checks, so the same check failing again pauses Auto again."
---

These are the engine half of the desktop app's review fixes (Agent-Field/codeaf-desk), which call `inbox.dismiss`
and the resume door.
