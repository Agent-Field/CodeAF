---
kind: added
title: a real-model end-to-end lane proving the manual is reachable through the live chat surface
pr: 309
surface: [chat, engine]
invalidates:
  - "session.Event's Output field and DisplayEntry's are DISPLAY COPIES capped at 4000 bytes (internal/session/loop.go's outputLimit), not the tool result. A test that asserts on either is asserting on the surface's own truncation: the 207 KB tasks page arrives there as 4022 bytes with no cut notice in it. The whole result lives in the conversation's transcript.jsonl and nowhere else, so an end-to-end assertion about what a tool returned reads the journal on disk."
  - "The manual's retrieval was measured on the person's exact words (internal/manual/plainquestions_test.go). The model does not send the person's words: it composes a query of its own, and the corpus is fragile to the difference — `who can see my files in aforge` ranks permissions first, `who can see my files` ranks it third of four, and `who can see my files privacy file access`, `who can see my files when I use aforge` and `privacy files who can see my workspace` miss the page entirely. Issue #307."
---

#293's four PRs each proved themselves in their own package. None of them proved
anything through the surface a person actually uses: whether a plain question
opens the manual at all, whether the bound holds on a real turn, whether the
figure a page quotes survives the trip into a reply.

This lane drives `session.Agent` directly against a real model, the way
cmd/aforge assembles one, and asserts on the JOURNAL — the tool calls the model
made and the results it was handed — never on its prose. A model saying "the
manual says five" is the exact claim the manual exists to make checkable.

Four scenarios: a plain privacy question reaches the permissions page and the
reply carries something only the manual says; the largest page comes back cut
under `bare.ResultByteCap`, naming its own headings, and the road out of the cut
is travelled; the crew's size is derived from `config.ModelTiers` and the reply
has to carry it; and the person's own `aforge manual` door runs with no key in
the environment, calling no model and spending nothing.
