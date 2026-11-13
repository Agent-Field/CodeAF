---
kind: fixed
title: Reduce repeated work in large histories and team changes
pr: 1925
surface: [chat, engine, docs]
invalidates:
  - "Activity layouts previously copied full task and conversation records into each line. Lines now share immutable task records, while live conversation overlays keep independent copies; the 5,120-task paint has a 4 MiB allocation regression budget."
  - "Disbanding a team previously scanned all memberships again for every reporting-home lookup. It now reuses the existing batched home reading before changing coordination, preserving history and unrelated memberships."
  - "Opening a settled store previously checked for a missing session once per message. It now checks distinct nonempty session IDs through the existing covering index; later legacy writers' missing rows are still discovered."
---

No dependency, schema, persistent cache, product setting, or public interface is
added. Team mutation trades transient indexed storage for fewer repeated scans;
store reopening still scans message IDs. Contributor guidance records the Matt
Pocock diagnosis, TDD, module-design, and independent review workflow.
