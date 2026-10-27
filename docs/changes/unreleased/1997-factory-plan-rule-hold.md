---
kind: added
title: A plan that touches a path a rule guards waits for a person, even in Auto
pr: 1997
surface: [engine]
invalidates:
  - "A repository's `start.ask_first` and `start.never` were read only before a job started on its own; a job a person started, and every plan, ran past them. A job now carries the ask-first and never lines it started under (`start_rules` on the wire, the repository's with the factory's own), and when its finished plan names a file one of them covers, the approval after the plan records the rule and file (`rule_hold`) and Auto will not pass it (`autonomy_why` says \"A rule asks first: …\")."
  - "A pattern that ends in a slash now covers everything under that folder, in a plan check and before a start alike: `touch auth/` and a never line `auth/` cover `internal/auth/login.go`. Before a start the pattern as written still matches too, so an issue that names the folder itself asks as before; only more asks, never fewer."
  - "A finished plan that declares no file list (or an ambiguous one) could not be checked. Under an ask-first path rule or a never line it now holds too: `rule_hold` is \"ask first · the plan names no files\"."
  - "An ask-first path was read in lower case (`touch src/Auth/` matched nothing in `src/Auth/`). Only the `touch`, `touches` or `path:` prefix is read without case now; the path is matched as written, before a start and in a plan alike."
---

The serving factory works the hold out itself when a worker's report brings
the approval after the plan, from the plan and the rules the job started under:
a worker's runner records it on its own copy, which a report does not carry.
The hold is cleared when the person answers the approval, and when the job is
started again or brought back.
