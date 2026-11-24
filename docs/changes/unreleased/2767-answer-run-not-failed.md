---
kind: fixed
title: a server Afi run an answer started is no longer Failed for ending on a reply
pr: 2767
surface: [engine, factory, docs]
invalidates:
  - "Was true: on an organization's server, the run an answered Ask started was Failed (\"Afi stopped before saying the task was done\") whenever it wrote no file of its own, even when it had set the task to wait on unfinished tasks or the asking run had already written the page (mac-studio, 2026-11-17, Release notes for v0.5). Is true now: a clean run that leaves the task waiting on unfinished work puts it in Up next, where its wait shows; a run that goes on after an answer or a restart counts the files the task's earlier run made, so it is Done with them as results. Only a run that wrote nothing, or broke off, is Failed."
---
