---
kind: fixed
title: An organization task Afi ran that wrote its page and ended is Done with the page as its result, not Failed
pr: 2730
surface: [factory, docs]
invalidates:
  - "An Afi task run on the organization's server that ended without calling ws_task_update was always settled Failed (\"Afi stopped before saying the task was done\"); now a clean ending that made or rewrote a file is Done with those files under Result, and only a run that wrote nothing or broke off is Failed."
---
