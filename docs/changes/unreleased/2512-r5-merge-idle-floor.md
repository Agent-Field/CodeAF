---
kind: fixed
title: A pull request an AgentField run opened can be merged, and an idle engine stops re-reading every job
pr: 2512
surface: [factory, engine]
invalidates:
  - "A job whose pull request a run on another machine opened (an agentfield step, or run_on) never recorded an approved commit, so Merge refused it forever with \"no approved commit is recorded for this job\" and All jobs kept it under Running while the runner footer said nothing was running. The factory's GitHub follow now records the head it first reads after the run ended as the job's HeadCommit (factory.PinDeliveredHead); a later push is \"commits you did not approve\". Such a job is still not watched for CI repairs."
  - "Every Store.Get/List/ListSaved read and decoded every job document from disk. A document is now decoded only when its file changed (os.SameFile, size, mtime), from a process-wide cache that answers deep copies (internal/factory/store/readcache.go)."
  - "An open job manager or step checked its budget and the person's Stop ten times a second for its whole life. It does so only while it owns work in flight (a turn, a running command, an unfinished task); otherwise once a second."
  - "The knowledge vault's build loop passed every 2 seconds forever. A pass that finds the same chats backs off to at most every 30 seconds; Build (StartBuild) wakes it at once."
  - "Nothing said which part of the engine's wrap-up was slow. Any part over a second is now written to stderr as \"codeaf app: leaving: <part> took <d>\"."
---

From the fifth live A+B run (2026-11-08): shipped SWE-AF jobs could not merge (E10), and the idle
engine sat at 21% of a core with 82 jobs (E3). On a linux rig with the same floor and six idle
managers the engine went from 5.8% to 1.6% of a core and from 1.7 GB to 8.3 MB read in five
minutes. The run's 45-second exit after SIGTERM did not reproduce on linux or macOS (with cells)
with a step mid-command; the wrap-up now names its slow part so the next live run can.
