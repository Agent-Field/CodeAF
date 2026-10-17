---
kind: fixed
title: Factory rule previews use the same explicit facts as intake
pr: 1860
surface: [engine]
invalidates: []
---

The start-rule preview now uses intake’s decision function. Explicit size labels and the job’s budget ceiling decide eligibility; model size and cost estimates no longer make the preview say “would start” when intake asks first. Wait reasons, item order and the response shape stay the same. Ask reasons now use intake’s own words, including “no size label” and the job budget against the rule’s ceiling. This remains a rules preview, without runner availability, permissions or setup holds.

One size resolver serves intake and the job field: both size:M and size/M are accepted, and the largest wins when several labels are present. A slash size label can now allow an explicitly labelled candidate inside the repository’s rules; conflicting small and large labels now hold it at the larger size. Legacy XS and XL spellings remain accepted.
