# Shipped product knowledge

These Markdown corpora are what codeaf looks up when explaining itself. This
README is developer guidance outside both corpora; it is not a product page.
The [repository documentation map](../../docs/README.md) covers developer design,
contributor rules and history.

## Choose the product corpus

| Corpus | Surface | Useful starting pages |
| --- | --- | --- |
| [chat/](chat/) | Chat in `internal/tui3` over `internal/session` | [Getting started](chat/getting-started.md), [capabilities](chat/what-i-can-do.md), [commands](chat/commands.md), [keys](chat/keys.md) |
| [pages/](pages/) | Resident surface in `internal/head` and `internal/resident` | [What codeaf is](pages/what-codeaf-is.md), [surfaces](pages/surfaces.md) |

A mechanism shared by both surfaces needs an explanation in each corpus's own
vocabulary. Do not merge the corpora or insert repository contribution rules into
product answers. Runtime project knowledge learned from user chats is a separate
system; its behavior is explained in [Memory](chat/what-i-remember.md) and
[project knowledge](chat/places.md#project-knowledge-inside-memory).

## Write for lookup and for people

Use one `#` title per page and self-contained `##` sections under about 2,000
characters. Put the words people ask in headings: headings are the retrieval
index. Each section should answer a concrete question without requiring the
previous section to be retrieved.

Document capabilities, commands, tools, keys, defaults, limits, refusals and
recovery in the same change that alters them. Quote visible strings accurately.
Remove obsolete denials when a feature becomes available. Explain user-visible
behavior here; implementation ownership and design rationale belong in
[developer docs](../../docs/ARCHITECTURE.md).

Keep existing page names stable unless retrieval evidence justifies a change.
Long pages can be split by user intent, but preserve search coverage and validate
both source and packed lookup. Do not perform an alphabetical reorganization of
these folders merely to match the developer documentation categories.

## Validate a manual change

```sh
make test-packed-manual manual-gates
```

Use the relevant existing retrieval tests as well. If a question reaches the
wrong page, improve its headings and content rather than weakening the test.
`make build` generates the ignored `chat.pack.gz` and `pages.pack.gz` archives;
commit source Markdown, not generated packs. See [manual.go](manual.go) for the
corpus loading and packing entry points.
