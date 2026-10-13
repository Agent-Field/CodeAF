---
kind: fixed
title: security audits distinguish incomplete analysis from a completed search
pr: 1815
surface: [chat, engine]
invalidates:
  - "Unicode session labels were also sent as machine affinity keys, which some model gateways rejected. Unsafe keys are now stable ASCII while the readable labels and selected models remain unchanged."
  - "Failed hunt calls contributed empty batches without measured failure accounting, so every failed hunt could end as a clean pass. Failed attempts now survive checkpoints and reports, make the analysis incomplete and retain findings from completed hunts."
  - "A partial audit result could conceal a later model refusal or run limit. Result-bearing stops now keep their failed or ceiling ending and mark Markdown, JSON and SARIF as incomplete without changing proof requirements."
---
