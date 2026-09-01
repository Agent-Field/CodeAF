---
kind: fixed
title: an answer asked from home renders as markdown, through the transcript's own renderer
pr: 216
surface: [chat]
invalidates:
  - "The `ask here` pane on home drew a settled answer as wrapped plain text, so `**bold**`, a leading `##` and backticks reached the screen literally. It now goes through `app.renderMarkdown` — the same door `render.go`'s `settledMarkdown` opens for the transcript — so an answer read on home and the same answer read in a conversation are one rendering."
  - "A reply row in that pane had no notion of being settled; the fix adds `exchangeRow.settled`, flipped only by `homeExchange.closeReply()`. Any code that put `ex.live` back to -1 by hand is now wrong: settling is a property of the boundary, and there are five of them."
---

Issue #213. The pane's answers were the one place on this surface that skipped the markdown
renderer, and the same model output therefore looked polished in a conversation and broken
on home. Routing it through the shared door also gets the phone tier for free, which is what
a forty-cell column with no horizontal scroll needs: a fence wraps instead of being cut and
a table stacks instead of being squeezed.

A reply still arriving stays plain, because that is what the transcript does — markdown of a
half-written sentence re-flows under the reader's eye. There is no promotion throttle here;
the pane's answers are short enough that the settle is the catch-up.
