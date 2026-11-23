---
kind: fixed
title: Every job's pull request carries its own file list, and the Brief never says verification
pr: 2515
surface: [engine, factory]
invalidates:
  - "A job's pull request on the wire carried no file list, so a job whose change never reached this computer (an AgentField or SWE-AF run, any run built elsewhere) had no `change` and the desk's merge card could only say it cannot see the files. Every followed pull request now carries `pr.files` (path, status added/modified/removed/renamed/..., line counts, `previous_path` for a rename), `pr.files_total` and `pr.files_head`, read from GitHub once per head and bounded at 300 files. A client shows the list only while `pr.files_head` equals `pr.head_sha`."
  - "The Brief writer could keep a heading such as \"What the verification found\". Both Brief prompts now forbid auditor, verdict, verified, verification, unverified and refuted, and every drafted heading and text is rewritten into plain words (`What the checks found`, checked, outcome, reviewer, disproved, unchecked) before the version is kept."
---

Found by the seventh live A+B run: a run's pull request removed the
repository's own `.codeaf/factory.yaml` and the merge card could not say so,
and its Brief headed a section with a word a person should never read.
