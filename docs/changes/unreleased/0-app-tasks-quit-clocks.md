---
kind: added
title: codeaf app pauses handed-over tasks, resumes work after a quit, and runs a suggested choice on a clock
pr: 0
surface: [engine]
---

Was true: a handed-over task (a moved reply, a run) answered "no such task" to
the app's pause, page and log; quitting the app left a reply and its tasks
reading interrupted and failed, with processes given two seconds; a choice with
a suggested answer waited forever in the app; a task could be renamed to one
file it touched ("note1.md").

Is true now: a handed-over task is listed, paused at its next step and resumed
(a person's hold on the whole run, `plandb.Store.HoldRun`), its log is its
worker's steps and its notes say when they were read. A quit writes down what
was mid-reply and what tasks ran, stops processes with a five-second grace and
lists them "stopped by quit", and the next start carries the reply on
("Resumed after quit") and picks the tasks up. A choice with a suggestion runs
on the app's proposed-task clock, and that clock's receipt names its length.
A single file name is never taken as a task's name. Tasks carry `group_title`.
