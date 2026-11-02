---
kind: added
title: Page AI model roles insight, insight_notes, insight_ask and insight_themes with a seeded default
pr: 2138
surface: [engine, chat]
invalidates:
  - "Was true: an app's page AI could only borrow an existing errand role such as title, and settings.models.get/set had no row for it. Is true now: an engine whose hello says `settings.models.insight` registers the errand roles insight, insight_notes, insight_ask and insight_themes, answers them as rows in settings.models.get (`default` on insight, `follows: insight` on the overrides) and takes their pins in settings.models.set, stored in the models.roles row."
  - "Was true: a role's ladder was its pin, its tier, then the session model. Is true now: an override registered under a family reads its own pin, the family's pin, then a default the engine answers at read time (never written), before the tier; insight's default is deepseek/deepseek-v4.1-flash on OpenRouter while OpenRouter is connected and the allowed models admit it. Roles without a family resolve as before."
---
