---
kind: changed
title: Afi on an organization's server is private to each person, and a task it works runs in its own seat
pr: 2711
surface: [factory, app]
invalidates:
  - "An organization's server Afi kept one conversation per workstream that everyone in it read; now each person has their own, every entry carries the person it belongs to, and factory.org_workstream_agent answers private: true."
  - "A task Afi worked on the server ran in the asking person's conversation; it now runs in its own seat (runTaskTurn) and never writes into anyone's pane."
---

The final Workstreams spec says the Afi pane is private to you, in a personal workstream and in an organization. The server agent is also told today's date in the asker's time zone.
