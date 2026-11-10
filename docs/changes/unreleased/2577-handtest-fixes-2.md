---
kind: fixed
title: A draft survives reopening from Home, remote task spend counts once, and kept branches are named
pr: 2577
surface: [chat, engine, remote, docs]
invalidates:
  - "Reopening a conversation from Home after Ctrl+C on the engine road showed an empty composer although the draft was saved, and the empty composer could then overwrite the saved words (the restore half was in v0.8.2 too). A cold open now restores the draft on every road, and a composer that never loaded the saved draft cannot write over it."
  - "Continuing a remote task's parent conversation on the computer that ran the task billed the task a second time there. Paid calls carry one identity (session.UsageCallID) from the worker through the remote record, and reconstruction never writes a row for a call the ledger already holds."
  - "#2567's notice for turns typed in an older codeaf went silent after the chat moved to another computer and back, because the copy-ready stamp was rewritten empty. The stamp is now carried forward."
  - "A late reconciled provider receipt did not reach Home's card, so Home showed less than /cost. It now does."
  - "Since #1893 folded routine endings, a landing whose work was kept on a branch was folded too, and nothing on screen named the branch. It stays visible and says branch kept · <branch>."
  - "The tmux suite waited for `Memories`, the ordinary greeting and an unfolded `✓ N tasks done`; the product says `Memory` (#1943), shows the starter screen on a first conversation (#680) and folds endings (#1893). The suite and tasks.md now say what the product draws."
---

Found by the second full pre-release hand test of dev, on Linux and macOS, including the live tmux suite.
