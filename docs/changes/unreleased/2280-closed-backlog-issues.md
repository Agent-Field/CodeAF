---
kind: fixed
title: codeaf removes closed GitHub issues from untouched Backlog
pr: 2280
surface: [engine]
invalidates:
  - "Lists and remembered GitHub update times can decide that a Backlog issue is closed."
---

Lists now nominate rows; a direct issue read is the sole authority for automatic
close and reopen. The row lock protects one shared untouched gate. Personal
edits, Look closer, paid arrival reads, spend, chats, answers, runs and queued
work prevent this path from moving a row. This path never stops work; the
separate ClosedForAutomation spend guard remains in place.

An explicit automatic-close mark permits reopen, and personal activity clears
it. Permanent issue IDs prevent repository-name reuse from moving old rows;
redirects do nothing. Start refuses a replacement issue without changing the
saved row; legacy pipelines differing from plain defaults count as personal edits. Old rows learn identity only within the read budget.
Failed reads, including missing or deleted issues, leave the row unchanged and
retry with backoff. No GitHub change-time comparison or open-read memory remains.

Each repository gets at most 40 direct reads per poll, oldest ready nomination first; failed nominations move behind waiting rows.
The scheduling set is rebuilt after restart, with periodic sweeps every ten
minutes. Closed history is never listed. Automatically finished rows are checked
again, so a close and reopen within one read gap corrects on a later poll without
starting a Done row. Large sets can take several polls to check.

Finished rows still skip paid arrival reads; a job with no work still gets no end
brief. Identity, automatic-close provenance and personal-touch fields stay on
the service and are documented in the wire codec's Lost list. Workers report
incremental changes through RunReport and ApplyReport, so their copies cannot
replace these fields. Starting work clears automatic-close provenance.
