---
kind: added
title: The factory's Trigger API — a token per source, messages and status from outside, signed webhooks, Go and TypeScript clients
pr: 0
surface: [factory, docs]
invalidates:
  - "Was true: only the factory's own access token (or the operator's session) could file outside work at POST /api/intake, so every tool that filed work held a key to the whole factory. Is true: `codeaf factory source add NAME [--callback URL] [--trust collaborator|stranger]` (and `factory.source_add|list|revoke`) mints a token that files, reads and messages only work whose source is NAME, at most at the source's trust (stranger by default); on every other path it is refused as an unknown secret."
  - "Was true: a job filed from outside GitHub had no way to be told more after it was filed. Is true: POST /api/intake/{source}/{id}/messages {author?, text} adds the words as a comment on the job, read by the next step's brief and handled once by the manager exactly as a GitHub comment is (a stranger's is context only), and answers {job, accepted}."
  - "Was true: an outside tool could not read how far its job was. Is true: GET /api/intake/{source}/{id} answers {job, state, steps_done, steps_total, route?, pr_url?, question?, updated}, the job wire's own facts."
  - "Was true: inbox.EventKind had three moments (pr_opened, merged, stuck) and a stop with a question was sent as stuck. Is true: it also has filed, started, question, done and message; a stop with a question is `question` (Jira still comments it), and a source with a callback is sent each as a signed webhook (X-Codeaf-Signature: t=…,sha256=…), kept in a durable outbox and retried until a 2xx for three days."
---

A registered source is the shape every tool without its own connector now
follows: `pkg/codeaftrigger` (Go) and `sdk/typescript/codeaf-trigger.ts` wrap
the three calls and the signature check, and
`pkg/codeaftrigger/examples/filer` files a ticket from standard input and
prints the webhooks that come back. GitHub's poller and its comments are
unchanged. The manual's page is "The factory's Trigger API".
