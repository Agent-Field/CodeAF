# Sales-report workflow: real terminal evidence

Tested combined candidate `41cf9cd0f`, including PRs #1562, #1603, #1604 and #1610. This is evidence for that exact candidate, not a claim about a later revision.

Goal: produce a reusable daily sales report from a CSV, correct refund handling, export the finished report and hand it over with reproducible commands. Six real user turns in one default hosted tmux chat produced summarize.py, a regression test, README, requirements and matching report/export artifacts. Independent rerun passed; EUR=5.30/count2 and USD=19.75/count2.

All 58 completed calls were OpenRouter `deepseek/deepseek-v4.1-flash`, status200, including the completed helper task and auxiliary calls. Receipt JSON lists every completed call, binary hash and stored spill sizes. There was no independent pool-judge call after #1610.

The first export was piped through tail by the model. The next user correction explicitly requested no pipe or redirection; the actual command became `python3 trace_export.py`, completed with EXPORT_COMPLETE, and retained exactly8,388,608 bytes. The handoff reran the full command successfully. Two full-output snapshots each remain at8MiB; the earlier tail-only snapshot is131,211bytes. Structured grep found the sole requirement in docs rather than the copy in runtime history. These are bounded synthetic trace batches around a genuinely generated/useful sales artifact; this does not claim production dataset scale or a machine-wide quota.

- report-screen.png: corrected cancellation/refund rules, passing regression and exact currency totals.
- export-screen.png: source-only requirement search and usable README/report handoff.
- final-screen.png: natural correction from piped output to direct verbose export; completion and matching receipt.
- handoff-screen.png: final commands, independently usable output and concise totals.
- workflow-finite.mp4: actual VHS terminal frames for the first five turns; capture starts during initial processing, about7seconds after the first prompt, and includes its result and subsequent corrections.
- handoff-finite.mp4: actual final follow-up in the same session, showing regenerated final report and tests/export completion.
- workflow.cast and handoff.cast: original terminal streams with real event timing. MP4 encodes sampled real terminal frames at8fps and is time-compressed under capture load; use casts for timing. No synthetic terminal frames or reconstructed UI were used. VHS default GIF encoding stalled; finite-frame MP4 encoding preserved the captured frames without the problematic background/palette filter.
- sales-report-project.tar.gz: finished source and outputs only; no .git, API keys, profile or runtime logs.

The earlier run on PR1603 alone exposed #1608 and is private diagnostic evidence, excluded from this publication set.
