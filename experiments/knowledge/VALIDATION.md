# Validation of the next knowledge iteration

Date: 2026-10-25. Fixed point: user-approved `a7f167c`, preserved by tag
`knowledge-base-working-2026-10-25`. Work remains on `knowledge-base`.

## Checks

- 63 focused tests pass at sourced-note and cited-recall interfaces, including
  persistent retry, concurrency, partition isolation, exact source windows,
  omission repair, membership retention, correction reversion, reversible topic
  changes and zero-call unchanged workflow replay.
- All six frozen live support contracts pass with
  `deepseek/deepseek-v4.1-flash`. The first run exposed a host-label/semantic-scope
  defect (3/6); the expectations were retained while the boundary was repaired.
- Browser checks pass for original-source citations, source-basis cues, topic maps,
  merged redirects, split choices, section navigation and mobile width, with no
  page errors. Repository `make test-quick` and JavaScript syntax checks pass.
- The updated private-data inspector contains 49 subject pages and five topic
  maps. All original canonical claim IDs remain present. Omission repair retains
  original source records; it does not forge new identities for old evidence.

## Recorded model costs

This development iteration recorded **$0.424802272** of OpenRouter usage:
**$0.345870412** in successful responses and **$0.078931860** in three known failed
review responses. This includes repeated tuning, baseline/failed checks, audits,
reviews and recall. It excludes earlier iterations, Codex run/subscription costs,
storage/hosting and any failures without reported usage. It is not an invoice
reconciliation or a production forecast.

For one measured two-source update with the same initial vault, compact context
reduced input from **14,059 to 4,722 tokens** (66.41%) and reported cost from
**$0.0056733 to $0.002841** (49.92%). The context/retrieval algorithm changed, so this
is a small paired observation, not general accuracy equivalence or a per-turn
price. The membership regression and frozen support contracts pass.

An unchanged full workflow invokes **zero model calls**. Source pushes and optional
proposal validation also invoke zero calls. Scheduled processing is separately
bounded; sparse traffic can still lead to one compilation per ready event. No
monthly estimate is extrapolated from human message counts.

## Limits

This is a local experimental runner over a permitted log snapshot, with two
host-recorded repository artifacts. Legacy agent reports remain reports, and
source-level times do not establish fact-level validity intervals. Six support
contracts do not prove complete capture or general answer accuracy. Independent
reviews are advisory and produced false findings; executable checks and manual
code inspection were used to assess them.

The workspace currently serializes a complete local vault at publication; local
I/O grows with corpus size even though model context is bounded. Production host
authentication, response hooks, daemon scheduling, normalized publication,
retention and a larger human-scored longitudinal corpus remain integration work.
Raw logs, model receipts, screenshots and detailed cost ledgers stay outside the
source repository.
