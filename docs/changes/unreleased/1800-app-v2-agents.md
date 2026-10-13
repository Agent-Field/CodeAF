---
kind: added
title: The app engine keeps agents and their lifecycle across relaunch
pr: 1800
surface: [engine, remote, docs]
invalidates:
  - "The app API served only transport and events. It now lists, creates, reads, patches, closes, archives and reopens agents using stable transcript ids."
  - "Every journaled conversation bought an AI title. App sessions now use the first message's words, capped at 40 Unicode characters, and explicit renames persist through the canonical engine wire."
---

A4a ports the bridge's attached-versus-saved session split into the app registry.
The shared engine home retains lifecycle and pin settings with atomic replacement
and a cross-process lock. Close checks running work and requires an explicit stop;
Closed and Archived retain transcripts forever. Stale expectations refuse before
mutating; keyed POST responses persist alongside state and replay without another
event. The TUI's own title behaviour remains available. Full turns, queue and
files belong to A4b, idle selection to the rule lane, and aggregate budget asks
to A11b.
