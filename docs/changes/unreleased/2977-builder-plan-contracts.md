---
kind: fixed
title: Keep app reuse choices, native actions, and planned run IDs through builds
pr: 2977
surface: [app]
invalidates:
  - "An app build could drop an accepted reuse choice, recreate a native capability as a run, or change a planned run ID. The builder now carries those choices through writing and repair."
---

The builder uses native CSV import, record lifecycles, approval and ordinary Add forms where they fit. Replies name apps, screens and parts in plain words. Build activity keeps the details, and the outline puts each reuse choice or starter on its data row.
