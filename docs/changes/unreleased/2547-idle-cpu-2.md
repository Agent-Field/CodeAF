---
kind: fixed
title: An idle app no longer reads every factory job on every look and every request
pr: 2547
surface: [engine, factory]
invalidates:
  - "Was true (#2545): the person-Stop manager index was rebuilt only when the floor folder's modification time changed. That folder also holds records a live engine rewrites every second or two, so on a real machine the index was rebuilt from a full read of every job on every look of every job chat. Is true now: the index answers until this process writes a job document or one second passes, and a recheck stats each job and reads only documents that changed."
  - "Was true: every factory HTTP request (settleJobs) and every worker pull (lease.Desk.Pull) opened and decoded every job document on the floor, and List read each document twice. Is true now: the store keeps decoded documents beside their file identity and decodes only a document whose file changed; a listing of an unchanged floor costs a folder listing, a stat and a copy per job."
  - "Was true: the diagnostic profile could only cover the start of a process. Is true now: CODEAF_DIAGNOSTIC_PROFILE_AFTER (0..3600 seconds) opens the window that long after start, so a steady state can be profiled."
---

On the owner's Mac (371 jobs, ~100 chats) an idle app went from 142/121/59/53% CPU
at 1/2/3/5 minutes to 28/45/21/14%, about 20% at 7-9 minutes. The rest is the
desktop's own polling reads and the knowledge pass; see PERF.md.
