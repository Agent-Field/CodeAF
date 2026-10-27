---
kind: added
title: Pick up issues — a factory on your own machine can start labelled issues by itself, if turned on
pr: 1997
surface: [engine]
invalidates:
  - "A factory served by a person's own machine never ran start rules: its poll brought issues to Backlog and nothing started without a person. The factory's own rules now carry a switch, `start_on_its_own` (off unless turned on), and `start_labels`. While it is on, that machine's poll runs the start rules after each read: an issue with a start label (the factory's or the repository's own `start.labels`) that clears every rule starts, and one a rule flags waits with \"Approval needed: start · …\". The setting is read again on every poll, so turning it off stops the next start."
  - "A team server's start rules are unchanged, except that the factory's `start_labels` join each repository's own while the switch is on."
  - "A job a start rule started had no person when its issue had no assignee, so Auto stopped at its first approval with \"A job owner is required\". It is now the factory owner's: the operator on a team server, the GitHub login of the person whose machine serves the factory otherwise, set as its starter (an assigned issue keeps its assignee). One a rule holds for approval stays nobody's, so any teammate who could approve it still can."
  - "`PUT /v1/rules` and `factory.rules_set` take `start_on_its_own` and `start_labels`, each optional: omitted keeps the factory's setting."
---
