---
kind: added
title: the app can read a specialist's report file byte for byte, on the receipt of the run that wrote it
pr: 0
surface: [engine]
invalidates:
  - "Was true: there was no way for the app to read a program's report; `EndingFacts.reports` were display names and the only file door (`files.read`) took a caller path, followed links, judged `outside` and stopped at 1 MiB. Is true now: the optional `tasks.program_report {id, launch, name}` answers the raw bytes (standard base64 string, at most 2 MiB, with `bytes` and `sha256`) of ONE report file the task's program wrote in the task's own record folder, for the record's current launch only, and ONLY ON THAT LAUNCH'S RECEIPT. `TaskProgram` gains `launch` and `report_files` to ask with. `EndingFacts.reports` remain display-only and are never a grant."
  - "Was true: a program's record (`delegate-program.json`) said nothing about which files its launch wrote, so a report file under a declared name could be an earlier run's (a rerun that failed before writing leaves it standing). Is true now: review (`review-report.json`) and sec (`sec-report.json`) issue a receipt `{name, bytes, sha256}` of exactly the bytes they wrote on their terminal line (`report_receipts`), and the program worker keeps the launch's own declared receipts on the record (`report_receipts`) in the write that sets `ended_at`. A launch with no receipt for a name (a failed rerun, a record from before receipts) has no report: `report_files` leaves it out and the read is `not_found`; nothing is backfilled from declarations, times or the file."
  - "Was true: nothing said which launch a report belonged to. Is true now: a launch that is not the record's, a file whose size or digest is not the receipt's (a rewrite in place included), a file, folder or record that changed while read (`conflict`); a run with no `ended_at` (`not_found`); a name the program did not declare (`bad_params`); a link, pipe, device, folder or second name, and a linked record folder (`engine`, the folder opened once with `O_NOFOLLOW` and every later step relative to it); a task not this agent's (`not_found`) and a file over 2 MiB (`too_large`) are each refused, never an empty or partial report. At most two reads run in the process, admitted before any I/O and held until the answer is built; a third is `busy` at once, a read past 3 s is `busy`, a closing app is `closed`. Local engine on linux and macOS only: a `--host` agent answers `unknown_method`, another platform `engine`."
---

No spend, routing, runner, cap or remote-protocol behaviour changes, and no
report's written bytes, names, modes or caps change: a receipt is computed from
the bytes already handed to the write. `/review post` is unchanged and is not
granted anything. Baseline note: this is PR 0, a preserved local milestone, not
ready for a public PR.
