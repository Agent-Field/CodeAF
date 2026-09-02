---
kind: added
title: aforge logs shows the whole row, filters it five ways, passes JSON through and opens a body
pr: 359
surface: [build, chat]
invalidates:
  - "`aforge logs` printed the tag, model, effort, ceiling, attempt, status, time, finish, tokens and cost and dropped the rest of the row. It now also prints the lane asked for and the lane that served (`auto→coreweave` when they differ, one name when they do not), the time to first token, the deadline the wait was held against, and the hedge facts (`acted hedge`, `2 arms`, `hedged`, `waste $0.0012`). Nothing it printed before disappeared."
  - "`aforge logs` had only `--tail`, `--follow` and `--path`. It now also takes `--tag`, `--model`, `--node`, `--call` and `--run` (exact matches that combine), `--json` (the file's own rows, unchanged, with no path header), and `--body <call id>`. `--call` is the only reading that shows both rows of an attempt, and there the outbound row reads `sent` rather than `⋯ in flight`."
  - "`--run` and `--body` are wired but cannot yet find anything: no writer puts a `run` field on a call-log row and neither `<logs>/trace/<run>/calls/<id>.json` nor `<logs>/failures/<id>.json` is written by anything today (#339, #343). So `--run` prints `no row in this log carries a run id yet` and `--body` prints `no body recorded for <id>`. Both begin working when their writer lands, with no further change to the reader."
  - "A filter that matched nothing used to be indistinguishable from a log with nothing in it. `--run` and `--call` now say which kind of nothing it is: `no row in this log carries a run id yet` when no row is stamped at all, `no calls for run <id>` when rows are stamped and none is that one, and `no call <id> in this log`. A tag, model or node that matched nothing still prints only the path, and `--json` prints nothing at all so it stays a pure passthrough."
---

The record already held the half of the story a person opens `aforge logs` to
see — which endpoint was asked for, which one answered, how long the first
token took, and what was done about a silence — and the reader dropped all of
it, so debugging a slow afternoon meant grepping the JSON by hand. The reader
now shows what the record holds and can find one run, one call, one tag, one
model or one node in it.
