# Queue vendor pricing schedule

**Effective date:** 2026-03-01
**Applies to:** all platform-group services

Charges are per published message, billed monthly. A billing month is 30 days
regardless of calendar length.

| queue technology | price |
|---|---|
| rabbit-legacy | 9 cents per 10,000 messages, flat |
| kafka-shared | **tiered**, see below |
| pulsar-edge | 6 cents per 10,000 messages, flat |

## Kafka volume tier

Kafka moved to tiered pricing at this renewal. The tier is assessed on the
**combined** monthly volume of every platform-group service on kafka-shared,
not per service — we buy it as one account and the discount is an account-level
one.

| monthly messages on kafka-shared | price |
|---|---|
| the first 300,000,000 | 4 cents per 10,000 messages |
| every message above 300,000,000 | 2 cents per 10,000 messages |

The threshold is a step in the rate, not a cliff: messages below it are billed
at the first-tier rate and messages above it at the second, in the same
invoice. There is no true-up and no retroactive rerate.

Volumes are taken from the service catalog's messages-per-day figure.

Rabbit pricing fell at renewal and pulsar is unchanged. Note that the cheaper
technology is not the strategic one — see the queue ADRs before drawing a
conclusion from this table.

No minimum commitment on any technology.
