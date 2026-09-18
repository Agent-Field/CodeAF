---
kind: changed
title: the relay carries the acceptable metric beside the judge's opinion
pr: 1129
surface: [build]
invalidates:
  - "`relay/src/schema.js` refused any row whose metric was not `role_quality`, and a cell key held five segments. The relay now accepts `acceptable` too — the harness's own model-free grade of a landing, 100 or 0 per seat, with the grader (`codeaf/grader`, `codeaf/grader-build`) in the row's judge column. A `role_quality` key keeps its five segments, so no stored triple moves; any other metric leads its key with its own name. `aggregate` groups graded cells per (role, model, source) with no judge severity removed and fits severity over judged cells only; the document declares both metrics and writes a graded cell's `source` with its vendor sliced off. THE RELAY DEPLOYS BEFORE THE CLIENT (#1123): an older relay refuses a batch holding an `acceptable` row and the client's outbox retries it until the relay learns the word. Relay tests: 38 pass, 0 fail; #1128's nonce dedup is untouched."
---

The relay half of #1123 on its own, so the Worker deploys from a `santos/dev`
head that maps to one commit. Six files under `relay/`, nothing else.
