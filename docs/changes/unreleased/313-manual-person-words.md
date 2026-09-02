---
kind: fixed
title: looking something up in the manual searches your own words, not only the ones the model composed
pr: 313
surface: [chat, engine]
invalidates:
  - "The manual's retrieval floors were the whole story. They are not: `internal/manual`'s twenty-five plain questions and its held-out set are ranked with the person's exact words and no model in the loop, and the model does not send those words — it composes a query of its own. What a person meets is now measured separately, through a live model, by `TestManualOnTheWire` in internal/e2e (build tag e2e), and that lane has its own floors."
  - "`Corpus.Search` was the only way into the corpus's ranking. `Corpus.SearchBoth(query, personsWords, k)` is now the one the belt tool calls, and internal/manual/theirwords.go owns the merge; the questions the floors are measured on moved out of plainquestions_test.go into the package internal/manual/asked so the free lane and the paid one cannot drift apart."
---

Asked "who can see my files", the model does not search those words. It composes a query
of its own — measured on deepseek-v4-flash: "who can see my files privacy file access",
"privacy files who can see my workspace" — and the manual is a few dozen short sections,
so two words nobody said drop the page out of the four the model is handed. The retrieval
#298 fixed was thinner on the wire than in its own tests.

The lookup now reads both: the query the model composed and the sentence the person
actually typed, which the conversation is already holding. Each side is searched, the two
results are merged, and a section both questions returned is ranked ahead of one only a
single question found. Nothing is asked of the model and no description tells it to quote
anybody, so a cheap model that paraphrases is no longer paraphrasing away the answer. A
message too long to be a question — a pasted document — is not read as one, and the
model's query stands alone exactly as it did before.
