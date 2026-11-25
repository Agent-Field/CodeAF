---
kind: changed
title: A task that only reads its folder says so when it starts, to its worker, and when it lands
pr: 2909
surface: [engine, chat]
invalidates:
  - "A task whose stand is a reference (TaskModeReference, a ground it only reads) got the same receipt as any task: \"It works in <ground>, the folder this proposal gave as its ground.\" or \"It works in <ground>, the one folder its brief names the work in.\" Outside the home folder its receipt now reads \"It reads <ground> and writes only in a folder of its own, so nothing in <ground> changes: this proposal named no file to make or change there. If the work is meant to change <ground>, stop this task and propose it again naming the files under <ground> it makes or changes.\" The home folder's sentence is as it was."
  - "A reference worker was told \"<ground> is yours to read, not to change. Everything you make goes in <dir>, your own folder. In your report, say where in that folder the results are.\" only when its ground held the home folder (taskCopy.readsHome, withHomeReadingNote); over any other ground it was told nothing. Every reference worker whose folder is not its ground is now told \"<ground> is yours to read, not to change. Everything you make goes in <dir>, your own folder, and stays there: nothing you make reaches <ground>. In your report, say where in that folder the results are, and never say the work is in <ground>.\" (taskReadsNote; the field is taskCopy.reads)."
  - "A run that only read its ground landed with no line of its own (RunLanding.Line empty, no branch), and its page's homecoming note read \"its work is in <ground> on <branch>\". Its landing line and that note now read \"it changed nothing in <ground>: what it made is in <its folder>\" (referenceLandingLine)."
  - "The manager's first law ended \"Asked what is happening, answer from the team.\" It now adds \"A task (propose_task) is not a member: use one only for work no member is here for, or when the person asks for a task, and never to get round a member that did not start. When a member did not start, team_send's answer says why: tell the person that.\" The law's earlier sentences are as they were."
---

A proposal that named a repository as its ground and no file by path was read as
work that only reads it. The worker built the project in its own folder, reported
it integrated, and the conversation told the person to run it from a repository
nothing had been written to.
