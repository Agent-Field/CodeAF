---
kind: fixed
title: An idle floor settles from memory, held jobs stop re-reading, a clean quit hands leases back
pr: 2519
surface: [engine, factory]
invalidates:
  - "settleJobs and scheduleJobTimer listed the floor with Store.List: per job two stats, two deep copies and a stat and read of runner-settings.json, every 5 s while a lease was held. They now use Store.ListShared, which reads the folder once and answers every job whose file is unchanged (os.SameFile, size, time, mode) from memory with no copy; an edited file, in place or by rename, is read again. Its items are shared and read-only."
  - "A held job's worker flush (every 500 ms) read the mirror's job (hydrated Get, deep-copied twice) and ran run.Diff, which decodes every step's output, even when nothing had moved. A flush that found nothing now remembers the mirror file's store.Stamp and the journal's thread.Version; the next flushes stat both and skip the read while both are unchanged, for at most a minute (quietTrust). A send forgets the quiet, and an accepted reply not yet acknowledged always reports."
  - "A runner that quit cleanly let its held leases run out: about a minute on a dead hold, then a running job was marked 'Runner went offline mid-step'. On a clean quit each job's worker now POSTs /v1/workers/leases/{lease}/handback (also /v1/runners/leases/{lease}/handback; lease.Desk.HandBack) after its runner has let the job go: a running job is queued at once with no failure, a carried publish or merge waits for the next pull, other states are kept. It is skipped, leaving expiry as before, when the step has not stopped, when results are retained but unconfirmed, or when the factory does not answer within 2 s (handBackWait)."
---

From the eighth live A+B round (2026-11-08). After #2517 the macOS idle engine still read 6.25% of a core, and its profile put the settle and the held-job flush at about a fifth of it.
