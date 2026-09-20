---
kind: changed
title: the commit co-author links to the CodeAF account, and assisted-by names the model
surface: [engine, docs]
invalidates:
  - "The commit trailer was `Co-Authored-By: codeaf <agentfield-bot@users.noreply.github.com>`. That bare-username noreply form renders as a dead mailto on GitHub; the address is now ID-prefixed (`267109073+agentfield-bot@users.noreply.github.com`), the form GitHub links to the CodeAF account and renders with its avatar."
  - "A commit carried one trailer line. It now carries two: `Assisted-by: CodeAF (<model id>)` above `Co-Authored-By: CodeAF <…>`. The assisted-by line is filled at the belt render (`internal/session/beltfacts.go`) out of `Config.Model`; the exec law still carries the co-author alone."
---

GitHub only links a co-author to an account when the email resolves, and for
accounts since mid-2017 that resolution is the numeric id in front of the
username. `agentfield-bot` is id 267109073, so the ID-prefixed noreply address
is the spelling that makes the co-author avatar click through to the account
instead of dead-mailtoing.

The `Assisted-by` line follows the convention Artsy's open RFC argues for:
`Co-authored-by` reads to GitHub as a teammate, while `Assisted-by` keeps the
human owner and makes the tool provenance. Naming the model there makes `git
interpret-trailers` able to answer who typed the commit, and the line is
formatted with the session's configured model rather than left to the model to
guess its own name. The fixed arm's prefix waiver rose by the 162 bytes the
wider belt fact costs; the lean shape never renders the attribution row.