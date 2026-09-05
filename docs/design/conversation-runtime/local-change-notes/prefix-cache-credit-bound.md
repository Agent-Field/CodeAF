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

## The attribution the length had to travel with (root review)

The first cut of this change read the lineage at settlement out of
`askFor(model)`, backed by `laneAsks` — a last-ask-per-model map written at
encode time. That map answers **"which request encoded most recently"**, which
is a different question from "whose answer is this".

One `Client` serves several requests at once — a fan-out's leaves, an errand
beside a turn, the two halves of a hedge. Encode A, encode B, and let A settle
first: the map hands A's answer B's lineage, and B's conversation is credited
with a prompt cache that A wrote. The mutex around the map made that
misattribution free of data races; it did not make it true. Correcting the
*length* while leaving the *lineage* looked up at the end would have filed a
newly-accurate number against the wrong conversation.

So the observation is now a small typed value, `settled{lineage, prompt,
cached}`, built at the settlement site by `settledFrom(ctx, usage)`:

- the lineage is `CacheKeyFrom(ctx)` — the settling request's own context, which
  is the only thing that knows whose call it is. This is the same read
  `noteEndpointAffinity` already does, so the affinity pin and the prefix note
  can no longer disagree about which conversation an answer belonged to;
- it replaces two trailing positional ints rather than adding a third;
- `laneAsks`, `lanesState`, `rememberAsk` and `askFor` are **deleted**. Nothing
  else read them, so there is no second, staler answer left behind.

One consequence, stated plainly: `Sighting.PromptTokens` is now the settlement's
observed prompt length instead of the pre-send estimate. It feeds
`lane.promptNoise`, a three-bucket weight on how much noise a TTFT reading
carries. A settlement whose frame reported no length now weights as the quietest
bucket — which is exactly what the field already produced whenever no ask had
been remembered for that model, and is a better failure than an estimate
belonging to a different request. Output and generation timings keep their
existing path untouched.

Both settlement paths — the whole-response epilogue and the streamed one,
including its no-first-token branch — build the observation the same way and
still record the served identity read off the decode.

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

Two ablations, each run in the session that made this change rather than left in
the tree:

- restoring `cachedTokens` to `return req.PromptTokens` fails
  `TestAGrownPromptEarnsNoCreditForTokensTheLaneNeverSaw` with "a grown prompt
  was credited 15502 cached tokens, want the 14972 the lane actually saw", and
  `TestALengthNobodyReportedEarnsNoDiscount`;
- restoring the lineage-from-last-encode attribution fails
  `TestAnAnswerIsAttributedToItsOwnRequestWhenTwoOverlap` with "the last answer
  was A's and its prefix note says \"conversation-B\"".

The overlap regression drives two real completions through the lanestub door on
one client with two lineages, A long and B one token, so B overtakes A and the
settlements arrive out of encode order. It is timing-shaped: the margin is
~120ms against ~20ms, and the sighting-order assertion fails loudly with a
message naming the margin if that ever stops holding.
