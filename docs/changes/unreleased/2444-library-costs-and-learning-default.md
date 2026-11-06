---
kind: changed
title: Library costs are identifiable and learning starts on unless a choice was saved
pr: 2444
surface: [engine, chat]
---

Was true: memory was off when a profile had no saved choice. Settings quoted a
single test job's memory cost percentage. Spending grouped memory calls into
ordinary day/model/owner cells without preserving their Library activity.

Is true now: an unset learning choice is on. Saved off stays off; malformed
profiles and invalid saved choices do not enable automatic model calls. The
shared settings description points to actual recorded spending instead of an
example percentage. The model, compiler roles, prompts and quality checks are
unchanged.

New usage records identify learning and explicit answers. `spend.report` keeps
these activities in separate cells inside its existing totals, under the same
filters and bounded report. Historical memory rows are marked unspecified;
non-memory rows omit the field. Unknown prices remain unknown, and Library
subtotals must never be added to the overall bill a second time. Cached work
still makes no provider call and creates no new charge.
