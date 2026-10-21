---
kind: fixed
title: Confirm deletion, stop owned services and bound saved harness reads
pr: 0
surface: [engine, chat, remote]
invalidates:
  - "agents.delete accepted only an id and deleted immediately. It now requires literal confirm true before calling its producer, and stops kept-running services before removing the conversation; a failed stop preserves the record."
  - "harness.list bounded its reply only after loading every saved page and latest trace. It now admits reads and bounds directory entries, rows and file bytes before loading."
  - "skills.list hid failed folder discovery behind a partial shelf. A capable host now returns the failure; older hosts explicitly keep their shelf-only list, and merged project skills retain project-first priority."
---

The shared permanent-deletion close also stops owned services, covering every
surface using that producer.
