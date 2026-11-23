---
kind: added
title: Create a paired always-on computer on your own Railway account with codeaf devbox
pr: 2081
surface: [chat, remote, docs]
invalidates:
  - "codeaf could reach a remote computer someone had already set up, but could not create one. `codeaf devbox up <name>` now writes a readable Terraform folder and creates a paired computer on the person's own Railway account; `status` and `down` inspect and remove it."
  - "A computer that only ran codeaf serve never received the identity's saved provider keys, so a --at chat there stopped on a missing key. serve now keeps keys, settings, shared memories and presence in step when started with CODEAF_SERVE_SYNC=1, which a dev box sets; without that variable serve behaves exactly as before."
  - "An engine whose first profile load failed kept returning that failure to every later connection. It now caches only a successful load, so a connection after the keys arrive works."
---

`codeaf devbox up`, `status` and `down` create, inspect and remove one private
project, one service and one persistent volume with the person's own
RAILWAY_TOKEN; the person pays Railway and owns the Terraform folder (default
`<codeaf home>/devbox/<name>/`). Up applies in three guarded steps: the box
without secrets, then a one-time pairing code with the box's remote key and
admission book (only the variable collection may change), then, once the
pinned remote key answers, a clear that removes those values from Railway and
from the state and deletes the backup that held them. The identity code still
never gives remote access; the laptop that ran up is the one admitted, and
`codeaf devices` and `--at` use the chosen name.

Up sends the creating computer's saved keys once the box answers, so the first
`--at` chat has them. The box pins its sync relay to the laptop's (each release
channel has its own relay), and its first install uses the laptop's own release
when they share a channel, so it does not depend on GitHub's API limit. Pairing
and serve behave as before for anyone who never runs devbox. An AWS module is in
the source but has never been applied, and the command does not offer it.
