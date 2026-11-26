---
kind: fixed
title: Restore Personal app planning and masked-field totals
pr: 2973
surface: [app, engine, chat]
invalidates:
  - The app Builder used to refuse definitions that declared restricted or PII fields. It now plans from schema and keeps saved sample values out of model context.
  - Restricted and PII app data used to require a separate destination allowlist in every case. The profile's exact chosen model destination now counts for its native Personal data when no explicit allowlist is installed.
  - Masked numeric fields used to refuse sums and averages. A person can now total those fields across rows they can access.
---

Personal app owners can view their own native confidential values when
no explicit field rule masks them. Hidden fields still cannot be totaled.
Agents and automations need unmasked inputs for totals. Connected-source
approvals and outside-harness refusals still apply.
