---
kind: fixed
title: a launch's pool writers stop when the process closes
pr: 1124
surface: [chat]
invalidates:
  - "The Model Pool's two start-up errands — the index refresh that writes `doc.json` beside its signature under the profile's pool directory, and the outbox push that opens the outbox and writes the install nonce there — were started fire-and-forget through `guard.Go`, which joins nothing at shutdown. A process that closed left them writing into a profile nobody was waiting for: a test whose profile is a temporary directory failed its own clean-up with `unlinkat …/pool: directory not empty`, and a door that reopened on another profile could write into a directory it no longer owned. They are now seated on one context and one `sync.WaitGroup` per profile (`cmd/codeaf/poolindex.go`), and `v3Process.closeAll` cancels and waits for them before it closes the conversations and the stores."
  - "The errands' own budgets did not keep this. A budget bounds one fetch, not the life of the goroutine, so a relay that did not answer left the refresh running past the close — the failure it caused was a race that passed on the rerun. The join, not the budget, is what makes the profile quiet once the process is gone."
---
