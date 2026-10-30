---
kind: fixed
title: Run workers act as their task, task rows name their worker, clock starts and report turns read correctly
pr: 0
surface: [engine, chat, app, docs]
invalidates:
  - "Workers a run seats (tasks that joined shared work) carried no CODEAF_AGENT_DID, so a tool recorded them as the folder they typed in. Each now carries the conversation's DID with its task's #w<n>; a part split off under a task acts as that task."
  - "A proposal its clock started could close before its task's update reached the app and leave the receipt `No longer needed`. The question's withdrawal now says it started (Withdrawal.started), and the receipt reads `Started on its own after Ns`."
  - "An owed landing's receipt showed the person's question above the report, so the person appeared to have said it twice. The receipt now shows the report; the model still reads the question."
---

App tasks carry `worker` (the `<did>#w<n>` the task's commands act as). When a
task ends, an app that keeps anything under that name, such as a writer's
lease, releases it; the engine cannot reach such stores itself.
