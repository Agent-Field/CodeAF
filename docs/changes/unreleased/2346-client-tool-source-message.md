---
kind: added
title: A client tool call names the message its turn answers
pr: 2346
surface: [app, chat]
invalidates: []
---

`agents.tool_call` carries an optional `source_message_id`: the durable id of
the message the running turn answers, the same string as `Entry.message_id`
on that message's chat entry. It is captured when the turn starts, so it is the
same after an engine restart and on a replayed call. A stopped turn asked again
carries its recorded message. Turns the engine starts itself, including a
carry-on after a quit or close, carry none. The hello feature
`agents.client_tools.source` is advertised with `agents.client_tools`. `turn`
remains the process-local counter.
