# ADR-0011 — pulsar-edge is the target; kafka-shared is deprecated too

**Date:** 2026-02-15
**Status:** Accepted — current
**Supersedes:** ADR-0007 (in full)

## Context

ADR-0007 named kafka-shared as the destination for everything on
rabbit-legacy. Since then the shared Kafka cluster has been the proximate cause
of two capacity incidents, our contract for it renews in 2026 Q4 at a price we
do not want to pay, and the media workload's year on pulsar-edge has been
uneventful at a fraction of the operational cost.

Continuing to migrate services *onto* a technology we intend to leave would be
paying twice.

## Decision

**Both rabbit-legacy and kafka-shared are deprecated as of this ADR's date.**
The single target technology for the platform group is **pulsar-edge**.

Concretely:

- A service is on a deprecated queue technology if its queue client is
  `rabbit-legacy` **or** `kafka-shared`.
- `pulsar-edge` is the only technology that is not deprecated.
- Services still on rabbit-legacy migrate directly to pulsar-edge. They do not
  stop at kafka-shared on the way; ADR-0007's intermediate hop is cancelled.
- Services on kafka-shared join the same migration queue, behind the
  rabbit-legacy ones, which are older and more fragile.

## Consequences

Every service in the catalog except the one already on pulsar-edge is now on a
deprecated technology. That is the honest reading of this decision and it is
the number that should be reported, uncomfortable as it is.

The 2026 H1 funding from ADR-0007 carries over. The scope does not: it is now
the whole estate rather than four services.
