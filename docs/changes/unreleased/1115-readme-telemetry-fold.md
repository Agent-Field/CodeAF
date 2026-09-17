---
kind: changed
title: The README telemetry section is one collapsed block at the end of Docs
pr: 1115
surface: [docs]
invalidates:
  - "README.md had a `## Telemetry` heading between Install and the product tour: the notice, the install.json paragraph and every off switch, 23 lines. There is no Telemetry heading now; the notice sits verbatim inside a `<details>` block at the end of the Docs section, with one sentence and the link to docs/TELEMETRY.md, and the rest lives only in docs/TELEMETRY.md."
  - "test/installer-telemetry.sh still pins the README to the notice: it reads the first ```text block containing `codeaf sends anonymous usage counts`, wherever it is in the file. Moving the block is fine; changing a character of it is not."
---

Install flows straight into the product. Anyone who asks about telemetry finds
the answer in one search, and nobody trying the tool has to read past it first.
