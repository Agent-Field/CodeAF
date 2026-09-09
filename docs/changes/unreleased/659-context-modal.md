---
kind: fixed
title: Folder and attachment browsing share one modal with live columns
pr: 659
# Optional. Which part of the repository this touches, so a reader can skip it.
# One or more of: build, chat, docs, engine, remote, resident
surface: [chat]
# Optional, and the reason this file exists. Every statement that WAS true and is
# not any more, written whole: what it was, and what it is now. A model reading
# this has to be able to check its own memory against it, so "branch names" is
# useless and "the trunk was chat-v3-task and no longer exists; work goes to dev"
# is the whole point. Leave the list empty if nothing anybody believed changed.
invalidates:
  - /folder no longer opens a remembered-folder list while bare /attach opens inline columns; both open the same bounded context chooser in browse mode.
  - The context chooser is no longer bottom conversation chrome with a read-only right directory pane; it is a modal whose painted geometry owns keyboard and mouse input across all columns.
---

Directory rows in the right pane open directly, file rows become the preview
subject, and empty folders say they are empty instead of leaving dead space.
