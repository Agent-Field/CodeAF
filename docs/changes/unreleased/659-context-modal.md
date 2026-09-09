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
  - A shortened folder breadcrumb could exceed a narrow modal by one cell, or by the cut marker width for an oversized leaf. Names now fit the remaining painted cells, and breadcrumb clicks preserve legal trailing spaces in filesystem paths.
  - /folder no longer opens a remembered-folder list while bare /attach opens inline columns; both open the same bounded context chooser in browse mode.
  - The context chooser is no longer bottom conversation chrome with a read-only right directory pane; it is a modal whose painted geometry owns keyboard and mouse input across all columns.
  - A press or scroll beside the modal could be routed to a list or confirmation row at the same height. The whole backdrop now consumes pointer input without changing the chooser or the conversation below.
---

Directory rows in the right pane open directly, file rows become the preview
subject, and empty folders say they are empty instead of leaving dead space.

The final Mac review raises ordinary filenames to reading ink and uses restrained colour on the small file-type marks; sizes and ancestry stay quieter. Distinct type shapes and plain-terminal fallbacks remain available without colour.

The folder action test checks the actual clicked directory and the complete path
sent to the engine independently of the display's bounded path abbreviation.
