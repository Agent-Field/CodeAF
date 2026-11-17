---
kind: added
title: A new workstream starts with Afi asking what it is for, then a setup card to change and accept
pr: 0
surface: [app, engine]
invalidates:
  - "New workstream opened a sheet that asked for a name first, and the new workstream then offered \"Draft the brief and tasks\", which sent Afi searching an empty workstream. Now the workstream is made at once with Afi's question already showing, and Afi proposes the whole setup as one card with ws_propose_setup."
  - "Afi could only make tasks one at a time and had no way to propose a workstream's name, goal, people, rules and plan together. ws_propose_setup keeps one card per person; Accept in the app applies it all at once, with Undo."
---

Owner decision 2026-11-17 §9. The tool is spelled once in internal/session/tools_workstream_setup.go,
served by the organization's server (the card is kept on the workstream for the person who asked,
read back as GET's setup_card, cleared with PATCH's drop_setup_card) and pinned by the desktop app
for a Personal workstream. A task's run cannot propose one.
