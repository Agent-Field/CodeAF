---
kind: changed
title: keep useful step descriptions visible between actions
pr: 653
surface: [chat, docs]
invalidates:
  - "Between tool calls an extra Working row took attention and space from useful captions. The latest completed caption now stays readable and static, with only a separate inline activity dot animated."
  - "Compact waiting repeated model names and early wait telemetry. A known response wait gains a dim awaiting response label and elapsed age only after the existing 10-second threshold; detailed phase and retry information remains in the footer and expanded view."
  - "The compact response-wait label could misdescribe connection recovery as a slow model. A reported connection loss now says waiting for connection immediately when space permits, while task pages continue to keep their parent's connection state out."
---

Active tool captions appear using the existing public narration or a description
composed from tool targets. The optional cheap narrator starts after a 500 ms
dwell while the batch runs; short batches can finish before it answers. It is
limited to three attempts per turn and 80 output tokens per attempt. A late,
cancelled answer is not installed as a new caption after the batch has ended. The first-caption
Working door, expansion controls, task-page isolation, and completion/reopen
collapse are unchanged. Waiting suffixes never reflow descriptions; when even
the dot cannot fit it occupies the existing icon gutter. Accessible and
lower-colour modes keep the dot still. No new model call or timer is introduced.
