# ADR-0007 — Consolidate on kafka-shared

**Date:** 2025-09-12
**Status:** Superseded by ADR-0011 (2026-02-15)

## Context

We run three queue technologies. rabbit-legacy predates the platform group and
nobody has operated it confidently since the team that installed it left.
kafka-shared is where most new work has landed. pulsar-edge was brought in for
one media workload and has stayed.

Three technologies is two too many.

## Decision

**rabbit-legacy is deprecated.** No new service may adopt it. Existing services
on rabbit-legacy migrate to **kafka-shared**, which becomes the standard queue
technology for the platform group.

pulsar-edge is tolerated for the media workload and is not a migration target.

## Consequences

- svc-auth, svc-billing, svc-notify and svc-reports must migrate.
- The migration is funded through 2026 H1.

---

> **Status note, 2026-02-15.** This ADR is superseded in full by ADR-0011. Do
> not use it to determine which technologies are deprecated; ADR-0011 changed
> that answer. It is retained for the record only.
