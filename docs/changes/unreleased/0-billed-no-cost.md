---
kind: fixed
title: codeaf retains billed usage after cancelled model calls
pr: 0
surface: [engine]
invalidates:
  - "Interim usage could suppress reconciliation even when no billing sink consumed it. Cancelled calls now retain measured usage as a fallback if a final receipt cannot be recovered, through the existing ledger funnel."
  - "Helper call records omitted provider generation identities. Call logs and usage rows now retain generation_id when supplied, allowing later provider audits."
---

Cancellation, stream errors and timeouts can occur after billable work. A recovered
provider receipt supplies the final charge; otherwise measured interim usage is
retained once. Without usage or a retrievable receipt the price remains unknown;
observed text estimates are telemetry only and are never invented spend.
