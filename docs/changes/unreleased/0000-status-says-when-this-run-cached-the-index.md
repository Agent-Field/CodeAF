---
kind: fixed
title: pool status's index line says what this run's own fetch did to the cache
pr: 0000
surface: [chat]
invalidates:
  - "`codeaf pool status` read the cache, printed the index line, and only then ran the probe that fetches and stores the index — so on a fresh profile the line said `no index cached yet · built-in seed of …` underneath a fetch that had just cached one, and a person had to run status twice to learn it landed. Status runs the probe before the index line now: the line describes the document this run holds, tailed with what the probe displaced — `index · … · cached now (was built-in seed)`, or `(was <old generated day>)` when it replaced a cached document."
  - "`pool status --json` built its `index` from the pre-fetch cache the same way. It carries `cached_now: true` beside `source: \"cache\"` on the one status whose probe stored the document, and the field is left out on every other answer."
---

`probePool` already pulled under `TTL 0` and stored whatever verified, and the
pull's `Result.Changed` already said whether that store was a document the cache
did not hold — `probeSummary` now carries it, and status reads the cache again
after the probe and says the document it now holds. A probe that stored nothing
(a relay that served the cache's own version, a 304, or an address that did not
answer) leaves the index line exactly as it was, and `pool show`, which asks no
address, is untouched.
