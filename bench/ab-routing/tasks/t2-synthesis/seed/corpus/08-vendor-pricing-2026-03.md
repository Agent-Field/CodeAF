# Queue vendor pricing schedule

**Effective date:** 2026-03-01
**Applies to:** all platform-group services

Charges are per published message, billed monthly. A billing month is 30 days
regardless of calendar length.

| queue technology | price |
|---|---|
| rabbit-legacy | 9 cents per 10,000 messages |
| kafka-shared | 4 cents per 10,000 messages |
| pulsar-edge | 6 cents per 10,000 messages |

Volumes are taken from the service catalog's messages-per-day figure.

Kafka pricing fell at renewal and rabbit pricing fell with it; pulsar is
unchanged. Note that the cheaper technology is not the strategic one — see the
queue ADRs before drawing a conclusion from this table.

No minimum commitment on any technology. Overage is billed at the same rate as
base usage; there is no tiering.
