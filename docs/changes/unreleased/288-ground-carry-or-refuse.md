---
kind: fixed
title: a landing carries your uncommitted work or names the files, and a writing turn becomes a task
pr: 288
surface: [engine, chat]
invalidates:
  - "A landing that git refused before it started used to quote git's first line, which ends in a colon with the file list on the lines after it, so the report read \"did not merge cleanly and was kept: error: Your local changes to the following files would be overwritten by merge:\" and named no files at all. It now parses that list and names them, and no refusal on any road can end in a bare colon."
  - "A seal-strip rebase that would not go used to be abandoned silently: nothing was journaled, nothing was flagged, and the merge then failed for a reason the report could not account for. It now says so, in the node's journal and on its report: \"its branch task/… still carries your own uncommitted work\"."
  - "A landing used to give up whenever the person had uncommitted changes in the files the merge would write. It now sets those changes aside, merges, and puts them back — and where it cannot put them back, it returns the checkout to exactly the commit and content it had and keeps the branch."
  - "A chat turn used to be able to edit the workspace for as long as it liked, governed only by the checkpoint ceiling at forty finished rounds, which counts reading and knows nothing about writing. A turn may now change two files or make five write calls inline; the write that would cross that moves the work onto a task through the ceiling's own road. Reads are still free in any number."
---

The ground a task is carved from is the ground it merges into. The ladder seals
the person's uncommitted tree into the task's base and leaves their checkout
alone, so the same hunks live in two places — and the landing has to be able to
merge into that ground rather than fail with a sentence naming nothing.
