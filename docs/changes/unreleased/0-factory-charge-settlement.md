---
kind: fixed
title: codeaf collects Factory charges after failed and unmetered calls
pr: 0
surface: [engine]
invalidates:
  - "A Factory stream's interim usage could prevent its final receipt from reaching the ledger and job total."
  - "Successful Factory calls without usage could disappear from Spending, and queue overflow could discard charge rows."
---

Factory jobs and their page helpers recover the final provider receipt after a
failed call or a successful answer without usage. Each recovered charge reaches
the job total and Spending once. Unknown prices remain unbilled markers, attributed
to their job, with no invented zero price. A full ledger queue writes through
instead of dropping charges; the caller waits at most 100 milliseconds.
Memory billing keeps its existing path. Historical gaps are not reconstructed.
