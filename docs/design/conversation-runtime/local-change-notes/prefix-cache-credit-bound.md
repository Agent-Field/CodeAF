# The prefix note carries a length

Local change note. Not a changelog entry, not a PR description.

## What was wrong

`internal/lane`'s prefix memory stored `{prefix, at}` — which conversation a lane
last served, and when. It stored no length. `cachedTokens` therefore answered
"how much of this request does that lane hold?" with the **whole current
prompt**, whenever the lineage matched inside `PrefixHold`.

A conversation grows between requests. A lane that answered at 14,972 prompt
tokens was credited the full 15,502 of the next request: the ~530 tokens
appended since had never been on the wire to it and could not be in its cache.
The frontier priced that phantom discount into the lane's score
(`frontier.go:197`, `PriceWithCache`).

This is an accounting error that needs no cache behaviour at all to be wrong. It
is independent of what any endpoint reports about cache reads.

## What changed

- `prefixNote` gains `tokens int` — the prompt length the lane was last **seen**
  at (`internal/lane/choose.go`).
- `RememberPrefix(id, prefix, tokens, at)` takes it. Negative is clamped to zero.
- `cachedTokens` returns `min(note.tokens, req.PromptTokens)`, and returns 0 when
  `note.tokens <= 0`.
- The length fed in is the **settlement's** `usage.prompt_tokens`, threaded
  `client.go` → `noteVelocity` → `noteLane` → `RememberPrefix`, via a nil-safe
  `promptTokensOf`. It is deliberately **not** `ask.prompt`, the adapter's
  pre-send four-characters-a-token estimate that sits two lines away in
  `noteLane` — a discount granted on a guessed schema size is a discount no
  usage frame agreed to.
- `RememberedPrefix(id)` is added as the exported read half, for the same reason
  `ForgetPrefixes` is exported: the transport that writes these notes is in
  another package. Nothing in the routing path calls it.

Unchanged on purpose: `PrefixHold`, lineage isolation, the 256-entry bound and
its oldest-first eviction, affinity release (`provider/affinity.go`), the
sampler, provider fallback (`AllowFallbacks`), prices, and exact-model pins.
`Sighting.PromptTokens` still carries the pre-send estimate — a different
consumer, out of scope here.

## Honest limits

**This is still an estimate, not a receipt.** A matching lineage inside the hold
says a lane answered these bytes recently — not that it still holds them. The
lane may have evicted the prefix. Only the next answer's `cached_tokens`
settles it, and nothing here reads that number back as proof.

**Compaction and rewrite are bounded, not solved.** They can change bytes
*under* an unchanged lineage, so a prompt of the same or shorter length may be a
*different* prompt that shares no prefix. The `min` keeps such a case from
earning credit for tokens it does not send; it cannot detect that the shared
prefix itself is gone. A lineage key that changed when the transcript was
rewritten would be the real fix, and that is not this change.

**Observed cache reads are not the cacheable prefix.** The last answer's
`cached_tokens` is deliberately not used as the note's length: a first cold call
reads zero and still *populates* the cache, so treating a cache-read count as
the ceiling would deny the discount exactly where it was earned.

**No cost claim.** The completed 36-cell benchmark ran on the preceding source.
Nothing here has been measured against it and no saving is claimed.

## Evidence

The growth case fails on the old behaviour and passes with the bound — the
ablation is recorded in the session that made this change, not in the tree.
