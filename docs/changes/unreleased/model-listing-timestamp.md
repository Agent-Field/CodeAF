---
kind: added
title: Model lists retain provider catalog listing timestamps
surface: [engine, app]
invalidates:
  - "models.list discarded the provider catalog's listing timestamp. Listed rows now optionally carry listed_at in Unix seconds, including through the picker cache; unknown, nonpositive, built-in and synthetic default timestamps stay absent. This is a provider listing time, not an inferred release date."
---
