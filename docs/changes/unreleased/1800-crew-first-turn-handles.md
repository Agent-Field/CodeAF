---
kind: fixed
title: crew members are addressable before their first turn
pr: 1800
surface: [engine, chat, docs]
invalidates:
  - "An unnamed crew member had no handle until its first title and could be skipped by everyone directives. Untitled members now receive a stable identity-based handle at admission, and their first directive can wake their first turn."
  - "A team_send could report success with no recipients or after a failed headless open. Empty delivery and failed opens now return tool errors with the available handles; directives saved before a failed open remain in traffic."
---

Crew handovers list message handles instead of agent ids. Existing title-based
and manager fallback rules remain, while identity handles survive later naming.
