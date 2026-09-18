---
kind: added
title: pool show --cells lists the held index's cells with their installs and dims
pr: 1149
surface: [engine]
invalidates:
  - "`codeaf pool show` answered what the held index holds as counts and one line per metric, and a person who wanted what a single cell is — which model, which role, what stood behind the measurement — had to parse `doc.json`. show now takes `--cells`: a `cells:` header after the metric lines, one line per cell — `role_quality · worker · vendor/model · mean 71.2 · sd 9.4 · n 42 · installs 5`, and for a metric whose cells are split by a dim, the dims between the model and the measurement, `acceptable · worker · vendor/model · source grader · share 0.83 · n 20 · installs 4`, the share word picked by the kind the document spells. The order is the index's own — metric, then role, model, dims — never a sort by a number, and a metric with no cells says `none`."
  - "`pool show --json --cells` carries the same cells as a `cells` array of `{metric, role, model, dims, mean, sd, n, installs}` beside the existing `index` summary; without `--cells` neither the words nor the object move."
  - "`internal/pool/index` read a cell's `installs` for the floor and dropped them. They are now carried on `Cell`, zero where the cell spells none — the shape the seed carries — so a reader can say what stood behind the measurement."
---

The flag, and not a fourth verb: the cells are part of what show shows —
the held document read one layer deeper, under the metric lines the form
already prints — so they ride show's own answer and its `--json` rather
than a `pool cells` verb that would carry a summary of its own. The dims
are said in the order the metric declares them, so `source` reads where
the document declares it, and a cell that spells no dim says nothing
between the model and the measurement.
