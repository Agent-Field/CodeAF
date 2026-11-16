---
kind: added
title: Workstream Asks have one decider each, take views, and record decisions that work is checked against
surface: [engine, app, chat]
invalidates:
  - "A workstream task had at most one Ask and only its owner answered it. A task now also carries `asks`, each with its own decider, and waits on all of them; `_task_answer` names the Ask with `ask`."
  - "Nobody but the owner could weigh in on an Ask except by comment. Anyone else now attaches one view each (`factory.org_workstream_task_view`); the decider never does."
  - "A workstream's decisions were kept by hand, proposed and kept. They now record themselves from an answered Ask, a request_note's `decision` and a rules change, carry `from`, `by_name`, and the state `withdrawn`, and only their maker withdraws or replaces one (`factory.org_workstream_decision_withdraw`, `_decision_replace`). A whole-workstream save that changes someone else's kept decision is refused."
  - "Nothing checked agent work against what a team decided. The session now checks `ws_task_create` and `ws_task_update {state: done}` against `ws_decisions` on the person's own model and pauses a conflict on an Ask to the decision's maker (`ws_task_conflict`, a door verb). Agents also list what they decided without asking under `assumed`, which Send back overturns (`overturn`)."
  - "Agents in a workstream carried eight tools. They carry nine: `ws_decisions` joins them."
---
