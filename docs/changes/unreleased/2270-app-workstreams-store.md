---
kind: added
title: codeaf app serves persistent workstreams with pages, records and go-aheads
pr: 2270
surface: [app]
invalidates:
  - Workstream methods previously answered unknown_method; they now serve persistent state and announce the workstreams feature.
---

Workstreams share collection identity, preserve page history, version plans,
and keep decisions, questions and bounded go-aheads in SQLite. Creating one
opens one bare agent without a first model turn; a repeated client id reuses it.
Factory jobs belong to at most one workstream, and changes notify watching apps.
List/get derive stage and numbers from bounded Factory reads and recorded agent
spend. Unknown readings are omitted; measured zero remains present. Mark done
ends active go-aheads and can archive member chats while keeping the agent record.
Go-aheads and wave auto-start settings are stored; Factory enforcement and
automatic starts are separate work.

Update, mark done and reopen results include the same available live numbers as
get. Single-line fields reject control characters; prose permits newline and
tab only. Marking an already done workstream remains unchanged.
