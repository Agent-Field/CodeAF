---
kind: changed
title: both doors that write a brief carry the handoff manifest, and the prefix came out lighter for it
pr: 179
surface: [engine, chat]
invalidates:
  - "`propose_task` did NOT take an `expects` manifest — PR #162 said so in its own body, `handoffcontract.go` said `today exactly ONE door carries it`, and a test asserted `taskSchemaJSON` must not contain the field. All three are reversed: `propose_task` takes the same optional `expects` `divide_work` takes, from the one constant that spells the schema, and a proposed task whose brief names something absent from the folder it gets lands `stale` before one model call."
  - "The manual said the assumptions come only from a task handing parts out under itself, and that a task the chat starts `is proposed against a folder the conversation can already see, and carries none`. It carries them: `internal/manual/chat/how-tasks-run.md` now says both doors do, and why a conversation is not always looking at the folder it proposes against."
  - "`prompts/system.md` taught four rules the tool block already carried in front of it — that `read` perceives PDFs and media, that `bash` waits and long-lived commands go to a background job, that `propose_task` is a contract in three parts, and that `tasks` marks other windows' work `another window`. Each is now stated once, in the description of the tool it is about; the prompt keeps only the clauses no tool description says."
  - "The fixed prefix was 47,996 bytes with four bytes of headroom, and that headroom was the reason the second door was refused. It is 47,941 with 59, under the same untouched `fixedPrefixBudget = 48_000`: the manifest's 1,230 bytes were paid for out of the system prompt, not borrowed from the budget."
---

The bill #162 left on the record, settled the way `prefixbudget_test.go` says to
settle one: *"take the bytes back out of something that repeats one, and leave
the budget where it is."*

Nothing downstream moved, because nothing had to — `taskSpec.expects`,
`TaskNode.Expects` and the preflight in front of the first worker were already
generic, and the only thing standing between a proposed task and a cents-cheap
stale landing was a property on a schema that could not afford it.
