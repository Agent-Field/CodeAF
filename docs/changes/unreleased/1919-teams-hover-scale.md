---
kind: fixed
title: Keep large team and Activity overviews responsive
pr: 1919
surface: [chat]
invalidates:
  - "Hovering All teams previously repeated membership searches across every team for each crew member. Each paint now shares one indexed reporting-home reading, and summaries avoid building full crew rows."
  - "Background spend reads previously reloaded and repaired unchanged team membership once per pool. They now share one immutable membership snapshot for the most recent profile, invalidated when its primary or legacy membership file changes."
  - "Activity/history previously copied every retained task on each filtering read, rebuilt the same layout for multiple readers in one paint, and repeatedly walked each task's ancestry. Unchanged rows are now shared, each paint shares its layouts, and tree construction memoizes parent validity and sizes sibling storage before filling it."
---

Reporting-home policy, spend accounting, and team limits retain their existing
behavior. Real terminal tests cover 100 teams with 100 members each and 200 teams
with 200 members each; these fixtures introduce no new product limits.
