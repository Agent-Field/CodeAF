---
kind: changed
title: the call log says who answered and what a failure cost, and a committed census reads it
pr: 852
surface: [engine, build, docs]
invalidates:
  - "`served` was empty on every row that never opened a stream, and a reader filling the gap from `lane` was reading the machine the preference ASKED FOR — the two disagreed on 3,728 of 10,107 finishes over the ten days to 2026-09-10. It is now the machine that ANSWERED: the stream's own `provider` field, or the single machine a request demanded (`provider.only` of one, or a rescue's demand). It is NEVER filled in from `lane`, and `provider.recordedServed` states that as a law."
  - "`deadline_ms` on a call-log row was read as the bound that applied to the attempt, and the first census called the field fiction on that reading — 7,937 rows saying 10,000 beside an `ms` that ran to 937,777, and 2,720 finishes apparently running past twice their own deadline. It was never a deadline: nothing ends a call when it passes, it is when the wait controller starts pricing a rescue. The field is now `hazard_ceiling_ms`; the figure is unchanged and every one of those 2,720 rows was a healthy call outliving a hazard ceiling. `aforge logs` spells it `rescue at 8.0s` where it said `deadline 8.0s`."
  - "A failed attempt recorded no cost and no tokens: 1,884 of 1,887 in-stream failures had `completion_tokens` absent and `cost` 0, so $201.15 of recorded spend had $0.00 attributed to anything that went wrong. Every row that ends an attempt now carries the usage that arrived — a cut stream, a 429 delivered inside an opened 200, a reply judged unusable — and `ttft_ms` whenever a first token came, on unwatched calls as well as raced ones."
  - "`retry_after` was never present on any row, on any of 1,106 paced refusals. It is now recorded in seconds from the `Retry-After` header wherever a refusal carried one, which is what makes a repeated send to the same machine checkable against the rule in docs/design/recovery/DESIGN.md §3."
  - "2,830 finishes carried no `tag` at all. `internal/session` opens no routing slot, so every auxiliary errand — naming a conversation, captioning, judging a route, sizing a task — landed in the log anonymous. An errand now tags its rows with its own role, which is 2,309 of them. The remaining ~500 are tool-time and resident calls and are not closed here."
  - "`cost_s` reached 1.99e+146 and the belief file had been refusing to compact for days on `json: unsupported value: NaN`. The cause was one unbounded expression: `Posterior.Predict` doubles variance every half-life with nothing above it, so a lane sitting still for about seven days overflowed to +Inf, and `Update`'s gain of P/(P+R) turned +Inf into NaN — which fails every `> ceiling` test, so nothing repaired it. `Predict` is now bounded at `lane.MaxSpread`, `Update` treats a belief that is not a number as no belief and adopts the observation, and `age` clamps unconditionally instead of skipping the clamp for every lane the public sheet publishes no spread for."
  - "`control.Measured` accepted any float. A figure that overflowed is now `PastPricing` — the state that already means \"a cost nobody could state\" — so nothing in the request path can put a number JSON cannot spell into the log. internal/calllog's finite.go stays the last line of defence and is not the road."
  - "The belief store marshalled whatever it held, and one NaN cost the whole of what the process had learned. It now drops the value: a belief whose numbers are not numbers is dropped whole the way a belief that names no lane already was, and the hierarchy levels, priors, quality tallies and workload estimates beside it are written."
  - "There was no committed census. `scripts/callcensus` was owed and the numbers in DESIGN.md §1 came from a throwaway pass. It is `cmd/aforge-census` and `make census LOG=… OUT=…`, with the Spark's nightly recipe in `bench/census/README.md`."
  - "`lanestub` could stage a full pool but not one that named its comeback time. `Profile.PacedFor` sets the `Retry-After` header on the immediate-429 transport."
---

The first of the five recovery waves, and it is measurement because two of that
design's twelve problems are that we cannot see. The four waves after it each
claim to move a number in §1's table; without an instrument that runs nightly
without anybody asking, every one of those claims is an argument about
anecdotes.

Run against the live log the census reproduces `census-20260910.md` within the
noise of a file that is still being written to.
