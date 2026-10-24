---
kind: fixed
title: Keep large teams overviews responsive
pr: 1919
surface: [tui]
invalidates:
  - "Hovering All teams previously repeated membership searches across every team for each crew member. Each paint now shares one indexed reporting-home reading, and summaries avoid building full crew rows."
  - "Background spend reads previously reloaded and repaired unchanged team membership once per pool. They now share one immutable membership snapshot for the most recent profile, invalidated when its primary or legacy membership file changes."
---

Reporting-home policy, spend accounting, and team limits retain their existing
behavior. Real terminal tests cover 100 teams with 100 members each and 200 teams
with 200 members each; these fixtures introduce no new product limits.
