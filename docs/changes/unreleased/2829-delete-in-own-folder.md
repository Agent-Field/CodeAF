---
kind: fixed
title: a recursive delete in the agent's own folder no longer asks because its path is absolute
pr: 2829
surface: [engine, chat, app]
invalidates:
  - "Was true: the recursive-delete floor (#2825) treated every absolute path as outside the agent's folder, including a `cd` to that very folder, because it read the command line alone; `cd /…/canvases/cv_x/work && rm -rf site-check/site-check`, run in exactly that folder, asked as `critical command` (irreversible, always asks) and blocked the turn. Is true now: the floor is told the folder the bash tool runs in (`approval.Policy.Workspace`, `approval.AlwaysAsksIn`, `config.ApprovalScopesIn`), and an absolute `cd` to that folder or below it, or an absolute target at or under it, is inside and runs without a question under an allow. The folder's own link is followed once; links inside a target are not."
  - "Was true: with a blanket allow, `rm -rf /<anything>` always asked through the table's `rm -rf /*` rows. Is true now: when the agent's folder is known, those four rows stand aside for the folder check, which still asks about `rm -rf /` and every delete it cannot place inside; `rm -rf /<agent folder>/build` runs. With no folder (`approval.AlwaysAsks`, `Policy.CheckBash` with no `Workspace`, the audit and plandb callers), the old answer stands."
  - "Still true: `~`, a variable or substitution, a `..` that climbs out, a folder that only shares the start of the name (`/tmp/a/work2` against `/tmp/a/work`), and any other absolute path ask as `critical command \"<the command as written>\"`."
---
