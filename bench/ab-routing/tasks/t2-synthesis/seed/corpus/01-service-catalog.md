# Service catalog — platform group

Maintained by team-platform. This file is the authoritative record of who owns
what, which queue technology each service speaks, and how much traffic it puts
through that queue. Last reviewed 2026-03-02.

"Messages per day" is the 28-day trailing mean of messages published, rounded
to the nearest ten thousand. It is the figure vendor invoices are reconciled
against.

---

## svc-auth — Authentication

- Owner: team-identity
- Tier: 1
- Queue client: rabbit-legacy
- Messages per day: 1,200,000

Session issuance and token refresh. Tier 1 because everything else fails when
it does.

## svc-billing — Billing

- Owner: team-payments
- Tier: 1
- Queue client: rabbit-legacy
- Messages per day: 400,000

Invoice generation and payment reconciliation.

## svc-catalog — Product catalog

- Owner: team-commerce
- Tier: 2
- Queue client: kafka-shared
- Messages per day: 2,500,000

## svc-notify — Notifications

- Owner: *(vacant — team-messaging was dissolved 2026-01-31 and no successor
  has been named)*
- Tier: 2
- Queue client: rabbit-legacy
- Messages per day: 900,000

Email, push and SMS fan-out. Operationally covered by the platform on-call
rotation as a stopgap; that is coverage, not ownership.

## svc-search — Search

- Owner: team-discovery
- Tier: 2
- Queue client: kafka-shared
- Messages per day: 5,000,000

## svc-media — Media processing

- Owner: *(vacant — transferred out of team-commerce 2026-02-10, no successor)*
- Tier: 3
- Queue client: pulsar-edge
- Messages per day: 150,000

Transcode and thumbnail pipeline. The first service to complete the migration
described in the queue ADRs.

## svc-reports — Reporting

- Owner: team-analytics
- Tier: 3
- Queue client: rabbit-legacy
- Messages per day: 60,000

## svc-gateway — Edge gateway

- Owner: team-platform
- Tier: 1
- Queue client: kafka-shared
- Messages per day: 8,000,000
