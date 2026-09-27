---
kind: added
title: completed job logs have a bounded retention budget
pr: 1599
surface: [chat, engine]
invalidates:
  - "Completed managed spools are retained under a per-directory limit of 128 MiB and 64 job groups, counting each base and rotation together. Active spools retain their independent per-job cap; legacy and unsafe files are preserved outside this budget."
  - "Job IDs use durable high-water metadata under a stable directory lock. Allocation creates the file, takes its active lease, and publishes ownership within the same transaction. Missing or damaged metadata refuses allocation instead of resetting IDs."
  - "A jobs footer checks for subsequent eviction and retains the in-memory output tail when the disk log is gone. Maintenance errors are reported without turning a completed job into a process failure."
---

Maintenance runs when a job claims its log, after its sink closes, and for
existing jobs directories during the startup sweep. It does not crawl the machine or run on every write.
Completed groups with the oldest allocated IDs are removed first. The cleaner
holds an independent file lease through removal; the writer must keep the same
main-log descriptor through rotation and spool failures until sink close.

Directory-relative operations reject symlinked jobs directories and ancestors
(except the platform's canonical temporary-directory alias). Metadata reads are
limited to 256 bytes; counter replacement uses an exclusive random temporary.
Ownership markers remain if a payload removal fails. Unmarked legacy logs may
have older live writers without leases, so they are never automatically removed.

The permanent lock records initialization, so a missing counter after all logs
have been evicted still refuses new allocation. Removing all allocation metadata
manually destroys that history and is not supported. On Windows the counter
file is flushed before rooted replacement; a directory flush is unavailable.

The startup TTL sweeper delegates `logs/jobs/` to this retention instead of
expiring the stable allocation metadata or ownership markers by age.
