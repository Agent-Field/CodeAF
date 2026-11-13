---
kind: fixed
title: Abandoned task fork folders no longer accumulate between launches
pr: 2352
surface: [chat, engine]
invalidates:
  - "Interrupted fork staging and orphaned model fork folders previously survived every launch. Closed conversations now remove abandoned folders untouched for more than seven days."
---
A failed task fork now removes staging for that exact destination before falling
back. Startup removes old staging and numeric task fork folders whose source task
directory is gone, including completed moving copies. Open conversations, recent
folders, symlinks and model forks with a remaining source task directory stay.
Every startup removal names its folder in the sweep note. This removes abandoned
folders only; furrow's global fork records and timelines are not purged.
