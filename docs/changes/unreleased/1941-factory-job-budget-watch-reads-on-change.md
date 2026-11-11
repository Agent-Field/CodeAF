---
kind: fixed
title: A factory job's open chat no longer re-reads the whole job ten times a second
pr: 1941
surface: [engine]
invalidates:
  - "Every open factory job or step chat checked the job's budget every 100 ms by reading and decoding the job's whole document, step log included, and trimming that log again. With a dozen chats open on jobs of a few hundred kilobytes this was over half of an idle engine's CPU. Each look now checks the file first and decodes the document again only when the file has changed; the budget line it answers is the same."
---

Measured
on a copy of a real factory home (296 jobs, the largest 543 KB) with no client
attached, as CPU time over 120 seconds after a minute's settling, twice each:
the engine before this change idled at 48.9% and 49.1% of a core, and after it
at 5.0% and 4.9%.
