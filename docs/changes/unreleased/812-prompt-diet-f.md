---
kind: changed
title: A tool description is a contract, and a schema law fails the build when one turns back into prose
pr: 812
surface: [chat, engine]
invalidates:
  - "`jobs` and `watch` no longer tell the model that handed-off work reports itself. That sentence was written four times across the belt (twice in `jobs`, once in `watch`, once in `propose_task`) and billed on every request of every turn; it is now stated once on prompts/system.md, and the manual answers it at length under `Does aforge poll a background job, or does it get told` (what-i-can-do.md). A lane looking for the law in a tool description will not find it there."
  - "Tool descriptions no longer carry `reach for this when` or `use X instead` sentences. `tasks` dropped `Use it when the person means earlier work without pointing at it`, its `continue` field dropped `A new propose_task is the wrong door`, its `stop` field dropped `Use it whenever the person says to stop, cancel or drop a task`, `read_document` dropped `use read for plain text, source and PDFs that have a text layer`, and `recall` dropped `so call it for an id or after one`. Routing is stated once, on the page's routing table."
  - "`internal/session/schemalaw_test.go` is a new build-blocking law over every tool the package can build: no ALL-CAPS run of three or more words in a schema string, no en or em dash in one, and no parameter description over 200 bytes. A field that genuinely cannot fit is registered in `schemaLawAllowed` with a reason and a byte cap; `propose_task`'s `brief` and `quick_task`'s judge are the registered judge paragraphs. Adding a paragraph to a tool schema is no longer something review has to catch."
  - "`read`'s senses sentence is 95 bytes rather than 322. It said `Images, audio and video are read the same way` and then enumerated what a picture, a recording and a video each come back as; it now says `Images, audio and video come back described, never as bytes; a scanned PDF needs read_document`. Both rules it carried survive: the answer is prose, and never write a decoder script."
  - "`commit` no longer says a belief `goes stale and leaves`. It says what the code does: a belief becomes stale and leaves recall."
---

The tool block is 24,044 bytes down to 22,823 with no rule dropped, because the
sentences that went were each said somewhere else already: the never-poll law on
the page, the routing rules in the routing table, and the worked examples in the
manual. What a description owes the model is the contract — what the tool does,
what its fields take, what its limits are and what comes back — and the schema
law is there so the next lane finds that out from a red build rather than from
this file.
